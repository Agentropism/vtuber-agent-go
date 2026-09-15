package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"onebot-gateway/agent/conversation/llm"
)

// 历史里的打断标记，与系统提示词中的说明必须一致。
const interruptedMarker = "[Interrupted by user]"

// interruptNotice 会追加到系统提示词末尾，告诉模型该标记的含义。
//
// 仅当打断标记以 user 角色写入历史时才需要（端点不支持在历史中段插入 system）。
const interruptNotice = "If you received `" + interruptedMarker + "` signal, you were interrupted."

// 未配置系统提示词时的兜底。
const defaultSystem = "You are a helpful assistant."

// defaultMaxToolRounds 是单轮对话内工具循环的默认上限。
const defaultMaxToolRounds = 8

// InterruptMode 决定打断标记以哪个角色写入历史。
type InterruptMode string

const (
	// InterruptAsUser 以 user 角色追加标记，适用于所有端点，是默认值。
	InterruptAsUser InterruptMode = "user"
	// InterruptAsSystem 以 system 角色追加标记，仅当端点支持在历史中段插入 system 时使用。
	InterruptAsSystem InterruptMode = "system"
)

// ChatClient 是本包需要的 LLM 能力，llm.Client 天然满足。
//
// 接口归调用方所有：本包只依赖「把一批消息发出去、把增量收回来」这一件事，
// 便于在测试中替换为脚本化的假实现。
type ChatClient interface {
	ChatStream(ctx context.Context, req llm.Request, onEvent func(llm.Event) error) error
}

// ToolExecutor 执行一批工具调用，并返回要回灌给模型的结果消息。
//
// 结果通常是一组 RoleTool 消息，每条带上对应的 ToolCallID。
type ToolExecutor interface {
	Execute(ctx context.Context, calls []llm.ToolCall) ([]llm.Message, error)
}

// AgentConfig 是构造 Agent 的参数。
type AgentConfig struct {
	LLM           ChatClient
	System        string        // 系统提示词，为空时使用兜底值
	Tools         []llm.Tool    // 声明给模型的工具，为空表示不启用工具
	ToolExecutor  ToolExecutor  // 模型要求调用工具时必须提供
	InterruptMode InterruptMode // 为空时按 InterruptAsUser 处理
	MaxToolRounds int           // 单轮对话内工具循环上限，<=0 取默认 8
}

// Agent 是带对话历史的会话智能体，对应原 Python 实现的 BasicMemoryAgent。
//
// 与原实现一致：Agent 自己持有历史与系统提示词，每次请求都把完整历史发给
// 无状态的 llm 客户端；模型要求调用工具时，先回灌一条带 tool_calls 的
// assistant 消息，再接工具结果，然后继续下一轮，直到模型不再要求工具为止。
//
// 未移植的部分（属于其它模块或已按执行计划裁剪）：
//   - MCP 提示词模式降级（计划 Q5：mcpp 重建为纯工具注册层，不做第二个循环）
//   - 长期记忆抽取与召回（E7）、句子分段与 TTS 预处理（E5）
type Agent struct {
	mu sync.Mutex

	client        ChatClient
	system        string
	tools         []llm.Tool
	executor      ToolExecutor
	interruptMode InterruptMode
	maxToolRounds int

	history          []llm.Message
	interruptHandled bool
}

// NewAgent 构造会话智能体。
func NewAgent(cfg AgentConfig) (*Agent, error) {
	if cfg.LLM == nil {
		return nil, errors.New("conversation: LLM 不能为空")
	}

	system := cfg.System
	if strings.TrimSpace(system) == "" {
		system = defaultSystem
	}

	mode := cfg.InterruptMode
	if mode == "" {
		mode = InterruptAsUser
	}
	if mode != InterruptAsUser && mode != InterruptAsSystem {
		return nil, fmt.Errorf("conversation: 未知的打断模式 %q", mode)
	}

	// 端点不支持在历史中段插入 system，因此只有在 user 模式下才追加说明。
	if mode == InterruptAsUser {
		system = system + "\n\n" + interruptNotice
	}

	rounds := cfg.MaxToolRounds
	if rounds <= 0 {
		rounds = defaultMaxToolRounds
	}

	return &Agent{
		client:        cfg.LLM,
		system:        system,
		tools:         cfg.Tools,
		executor:      cfg.ToolExecutor,
		interruptMode: mode,
		maxToolRounds: rounds,
	}, nil
}

// Chat 处理一轮用户输入，把回复文本以增量形式回调 onText。
//
// onText 返回非 nil error 会立即中止本轮并原样返回（用于调用方主动停止生成）。
// 模型要求调用工具时会自动完成「执行工具 → 回灌结果 → 继续生成」的循环。
//
// 本轮的用户消息一定会发给模型；是否写入历史要过一遍去重规则（与原实现一致）。
func (a *Agent) Chat(ctx context.Context, userText string, onText func(string) error) error {
	if onText == nil {
		return errors.New("conversation: onText 不能为空")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.interruptHandled = false

	userMessage := llm.Message{Role: llm.RoleUser, Content: userText}

	messages := make([]llm.Message, 0, len(a.history)+1)
	messages = append(messages, a.history...)
	messages = append(messages, userMessage)

	a.appendMessage(userMessage)

	for round := 0; ; round++ {
		if round >= a.maxToolRounds {
			return fmt.Errorf("conversation: 工具调用轮次超过上限 %d", a.maxToolRounds)
		}

		var (
			turnText    string
			pendingCall []llm.ToolCall
		)

		err := a.client.ChatStream(ctx, llm.Request{
			Messages: messages,
			System:   a.system,
			Tools:    a.tools,
		}, func(event llm.Event) error {
			if event.Text != "" {
				turnText += event.Text
				return onText(event.Text)
			}
			if len(event.ToolCalls) > 0 {
				pendingCall = event.ToolCalls
			}
			return nil
		})
		if err != nil {
			// 与原实现一致：本轮出错的助手文本不进历史。
			return err
		}

		if len(pendingCall) == 0 {
			a.appendMessage(llm.Message{Role: llm.RoleAssistant, Content: turnText})
			return nil
		}

		if a.executor == nil {
			return errors.New("conversation: 模型要求调用工具，但未配置 ToolExecutor")
		}

		// 先回灌带 tool_calls 的 assistant 消息，再接工具结果，顺序不能颠倒。
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   turnText,
			ToolCalls: pendingCall,
		})
		a.appendMessage(llm.Message{Role: llm.RoleAssistant, Content: turnText})

		results, err := a.executor.Execute(ctx, pendingCall)
		if err != nil {
			return fmt.Errorf("conversation: 执行工具失败: %w", err)
		}
		messages = append(messages, results...)
	}
}

// Interrupt 记录一次用户打断。
//
// 与原实现一致：把最后一条助手消息改写为「实际听到的部分 + …」，再追加一条打断标记。
// 每轮 Chat 只生效一次，Chat 开始时重置。
//
// 调用顺序必须是「先取消 Chat 的 ctx，再调用 Interrupt」：Chat 持有内部锁，
// ctx 取消后 ChatStream 会立刻返回并释放锁，Interrupt 随即完成改写，不会与
// 本轮尚未结束的助手文本写入交错。
func (a *Agent) Interrupt(heard string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.interruptHandled {
		return
	}
	a.interruptHandled = true

	if n := len(a.history); n > 0 && a.history[n-1].Role == llm.RoleAssistant {
		a.history[n-1].Content = heard + "..."
	} else if heard != "" {
		a.history = append(a.history, llm.Message{
			Role:    llm.RoleAssistant,
			Content: heard + "...",
		})
	}

	role := llm.RoleUser
	if a.interruptMode == InterruptAsSystem {
		role = llm.RoleSystem
	}
	a.history = append(a.history, llm.Message{Role: role, Content: interruptedMarker})
}

// History 返回历史的副本，调用方修改它不会影响 Agent。
func (a *Agent) History() []llm.Message {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]llm.Message, len(a.history))
	copy(out, a.history)

	return out
}

// System 返回实际生效的系统提示词（含打断说明）。
func (a *Agent) System() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.system
}

// Reset 清空对话历史。
func (a *Agent) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.history = nil
	a.interruptHandled = false
}

// appendMessage 追加一条历史消息。
//
// 沿用原实现的两种去重：助手空文本不入历史；与上一条角色和内容完全相同的消息不入历史。
// 后者会把「连续两条一模一样的用户消息」中的第二条挡在历史之外——但该消息仍会
// 发给模型，因此本轮不会空转。
func (a *Agent) appendMessage(message llm.Message) {
	if message.Role == llm.RoleAssistant && message.Content == "" {
		return
	}

	if n := len(a.history); n > 0 {
		last := a.history[n-1]
		if last.Role == message.Role && last.Content == message.Content {
			return
		}
	}

	a.history = append(a.history, message)
}
