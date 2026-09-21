package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// 消息角色。system 通常由 Request.System 承载，也可直接放进 Messages
// （例如打断标记需要以 system 角色插入历史中段时）。
const (
	RoleSystem    = openai.ChatMessageRoleSystem
	RoleUser      = openai.ChatMessageRoleUser
	RoleAssistant = openai.ChatMessageRoleAssistant
	RoleTool      = openai.ChatMessageRoleTool
)

// 默认采样温度，与配置缺省值一致。
const defaultTemperature = 1.0

// Config 描述一个 OpenAI 兼容端点。
type Config struct {
	BaseURL     string  // 例如 https://api.deepseek.com/v1
	APIKey      string  // 为空时仍可构造客户端，错误推迟到首次请求
	Model       string  // 例如 deepseek-chat
	Temperature float64 // 0 表示使用默认值 1.0
}

// Message 是一条对话消息。
//
// 字段刻意不暴露第三方 SDK 类型，便于日后替换实现。
type Message struct {
	Role       string     // user / assistant / tool / system
	Content    string     // 文本内容
	ToolCalls  []ToolCall // role=assistant 且本轮触发了工具调用时回填
	ToolCallID string     // role=tool 时必填，对应上一轮 assistant 的调用 ID
}

// ToolCall 是一次完整的工具调用，流式分片已按 index 累积完毕。
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON 字符串，模型可能返回空串
}

// Tool 描述一个可供模型调用的工具。
type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage // JSON Schema；为空时下发空对象 schema
}

// Event 是一次流式增量：文本片段或一轮完整的工具调用，两者不会同时非空。
type Event struct {
	Text      string
	ToolCalls []ToolCall
}

// Request 是一次补全请求。
type Request struct {
	Messages []Message
	System   string // 非空时插入到消息列表头部
	Tools    []Tool // 为空时不向模型声明工具
}

// Client 是某个 OpenAI 兼容端点的客户端，可并发使用。
type Client struct {
	api         *openai.Client
	model       string
	temperature float32
}

// New 按配置构造客户端。
//
// 不设置 HTTP 客户端超时：流式响应可能持续很久，超时应由调用方通过 ctx 控制。
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("llm: base_url 不能为空")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("llm: model 不能为空")
	}

	sdkCfg := openai.DefaultConfig(cfg.APIKey)
	sdkCfg.BaseURL = cfg.BaseURL

	temperature := cfg.Temperature
	if temperature == 0 {
		temperature = defaultTemperature
	}

	return &Client{
		api:         openai.NewClientWithConfig(sdkCfg),
		model:       cfg.Model,
		temperature: float32(temperature),
	}, nil
}

// Model 返回当前使用的模型名。
func (c *Client) Model() string {
	return c.model
}

// ChatStream 发起一次流式补全，逐段回调 onEvent。
//
// 文本片段边收边回调；工具调用会累积到一轮完整后回调一次（见 toolCallAccumulator）。
// onEvent 返回非 nil error 会立即中止请求并原样返回该 error，用于调用方主动停止生成。
// ctx 取消同样会中止请求。请求或读取失败一律返回 error，不会以正文形式回退。
func (c *Client) ChatStream(ctx context.Context, req Request, onEvent func(Event) error) error {
	if onEvent == nil {
		return errors.New("llm: onEvent 不能为空")
	}

	sdkReq := openai.ChatCompletionRequest{
		Model:       c.model,
		Messages:    toSDKMessages(req.Messages, req.System),
		Temperature: c.temperature,
	}
	if len(req.Tools) > 0 {
		sdkReq.Tools = toSDKTools(req.Tools)
	}

	stream, err := c.api.CreateChatCompletionStream(ctx, sdkReq)
	if err != nil {
		return fmt.Errorf("llm: 发起流式请求失败: %w", err)
	}
	defer func() {
		_ = stream.Close()
	}()

	accumulator := newToolCallAccumulator()

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("llm: 读取流失败: %w", err)
		}
		if len(resp.Choices) == 0 {
			continue
		}

		delta := resp.Choices[0].Delta
		if delta.Content != "" {
			if err := onEvent(Event{Text: delta.Content}); err != nil {
				return err
			}
		}
		accumulator.add(delta.ToolCalls)
	}

	// 流正常结束时若仍有累积的工具调用，作为一轮完整调用抛出。
	if calls := accumulator.drain(); len(calls) > 0 {
		return onEvent(Event{ToolCalls: calls})
	}

	return nil
}

// toolCallAccumulator 按 index 累积流式下发的工具调用分片。
//
// OpenAI 兼容端点会把一次工具调用的 id / function.name / function.arguments
// 拆到多个 chunk 里下发，其中 arguments 必须按到达顺序做字符串拼接。
type toolCallAccumulator struct {
	order []int
	byIdx map[int]*ToolCall
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{byIdx: make(map[int]*ToolCall)}
}

// add 吸收一个 chunk 中的工具调用分片。
//
// 端点未填 index 时退化为按分片在数组中的位置归并，这只能覆盖单路调用的场景。
func (a *toolCallAccumulator) add(deltas []openai.ToolCall) {
	for position, delta := range deltas {
		index := position
		if delta.Index != nil {
			index = *delta.Index
		}

		current, ok := a.byIdx[index]
		if !ok {
			current = &ToolCall{}
			a.byIdx[index] = current
			a.order = append(a.order, index)
		}

		if delta.ID != "" {
			current.ID = delta.ID
		}
		if delta.Function.Name != "" {
			current.Name = delta.Function.Name
		}
		current.Arguments += delta.Function.Arguments
	}
}

// drain 取出一轮完整调用并按首次出现顺序返回，同时清空累积状态。
func (a *toolCallAccumulator) drain() []ToolCall {
	if len(a.order) == 0 {
		return nil
	}

	calls := make([]ToolCall, 0, len(a.order))
	for _, index := range a.order {
		calls = append(calls, *a.byIdx[index])
	}

	a.order = nil
	a.byIdx = make(map[int]*ToolCall)

	return calls
}

// toSDKMessages 把内部消息转换为 SDK 消息，system 非空时置于列表头部。
func toSDKMessages(messages []Message, system string) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(messages)+1)

	if strings.TrimSpace(system) != "" {
		out = append(out, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: system,
		})
	}

	for _, message := range messages {
		converted := openai.ChatCompletionMessage{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
		}
		for _, call := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, openai.ToolCall{
				ID:   call.ID,
				Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
		out = append(out, converted)
	}

	return out
}

// emptyParameters 是无参数工具使用的 JSON Schema。
var emptyParameters = json.RawMessage(`{"type":"object","properties":{}}`)

// toSDKTools 把内部工具声明转换为 SDK 的 function tool。
func toSDKTools(tools []Tool) []openai.Tool {
	out := make([]openai.Tool, 0, len(tools))

	for _, tool := range tools {
		parameters := tool.Parameters
		if len(parameters) == 0 {
			parameters = emptyParameters
		}
		out = append(out, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  parameters,
			},
		})
	}

	return out
}
