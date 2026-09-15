package conversation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"onebot-gateway/agent/conversation/llm"
)

// scriptedClient 按预设脚本逐轮返回事件，并记录每一轮收到的请求。
type scriptedClient struct {
	mu    sync.Mutex
	turns [][]llm.Event
	calls []llm.Request
	err   error
}

func (c *scriptedClient) ChatStream(
	ctx context.Context,
	req llm.Request,
	onEvent func(llm.Event) error,
) error {
	c.mu.Lock()
	c.calls = append(c.calls, llm.Request{
		Messages: append([]llm.Message(nil), req.Messages...),
		System:   req.System,
		Tools:    append([]llm.Tool(nil), req.Tools...),
	})

	var events []llm.Event
	if len(c.turns) > 0 {
		events = c.turns[0]
		c.turns = c.turns[1:]
	}
	err := c.err
	c.mu.Unlock()

	if err != nil {
		return err
	}

	for _, event := range events {
		if err := onEvent(event); err != nil {
			return err
		}
	}
	return nil
}

func (c *scriptedClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *scriptedClient) callAt(t *testing.T, index int) llm.Request {
	t.Helper()

	c.mu.Lock()
	defer c.mu.Unlock()

	if index >= len(c.calls) {
		t.Fatalf("第 %d 次请求不存在，实际只有 %d 次", index, len(c.calls))
	}
	return c.calls[index]
}

// fakeExecutor 记录被调用的工具并返回预设结果。
type fakeExecutor struct {
	mu      sync.Mutex
	calls   [][]llm.ToolCall
	results []llm.Message
	err     error
}

func (e *fakeExecutor) Execute(ctx context.Context, calls []llm.ToolCall) ([]llm.Message, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.calls = append(e.calls, calls)
	if e.err != nil {
		return nil, e.err
	}
	return e.results, nil
}

func (e *fakeExecutor) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

func textEvents(parts ...string) []llm.Event {
	out := make([]llm.Event, 0, len(parts))
	for _, part := range parts {
		out = append(out, llm.Event{Text: part})
	}
	return out
}

func newTestAgent(t *testing.T, client ChatClient, cfg AgentConfig) *Agent {
	t.Helper()

	cfg.LLM = client
	agent, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("构造 Agent 失败: %v", err)
	}
	return agent
}

func collectText(t *testing.T, agent *Agent, userText string) []string {
	t.Helper()

	var chunks []string
	err := agent.Chat(context.Background(), userText, func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("Chat 返回错误: %v", err)
	}
	return chunks
}

func historyStrings(agent *Agent) []string {
	var out []string
	for _, message := range agent.History() {
		out = append(out, message.Role+":"+message.Content)
	}
	return out
}

func TestNewAgentValidation(t *testing.T) {
	if _, err := NewAgent(AgentConfig{}); err == nil {
		t.Error("缺少 LLM 时应报错")
	}

	if _, err := NewAgent(AgentConfig{
		LLM:           &scriptedClient{},
		InterruptMode: InterruptMode("bogus"),
	}); err == nil {
		t.Error("未知打断模式时应报错")
	}
}

func TestAgentChatAppendsHistory(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("你", "好")}}
	agent := newTestAgent(t, client, AgentConfig{})

	chunks := collectText(t, agent, "在吗")

	if strings.Join(chunks, "") != "你好" {
		t.Errorf("文本增量不符合预期: %v", chunks)
	}
	got := historyStrings(agent)
	want := []string{"user:在吗", "assistant:你好"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("历史不符合预期: %v", got)
	}
}

func TestAgentSecondTurnCarriesHistory(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("一"), textEvents("二")}}
	agent := newTestAgent(t, client, AgentConfig{})

	collectText(t, agent, "第一句")
	collectText(t, agent, "第二句")

	second := client.callAt(t, 1)
	var got []string
	for _, message := range second.Messages {
		got = append(got, message.Role+":"+message.Content)
	}
	want := []string{"user:第一句", "assistant:一", "user:第二句"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("第二轮请求未带上完整历史: %v", got)
	}
}

func TestAgentToolLoop(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{
		{
			{Text: "我查一下"},
			{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "query_status", Arguments: `{}`}}},
		},
		textEvents("状态正常"),
	}}
	executor := &fakeExecutor{
		results: []llm.Message{{Role: llm.RoleTool, ToolCallID: "c1", Content: "ok"}},
	}
	agent := newTestAgent(t, client, AgentConfig{
		Tools:        []llm.Tool{{Name: "query_status"}},
		ToolExecutor: executor,
	})

	chunks := collectText(t, agent, "查一下状态")

	if strings.Join(chunks, "") != "我查一下状态正常" {
		t.Errorf("跨越工具轮的文本增量不符合预期: %v", chunks)
	}
	if client.callCount() != 2 {
		t.Fatalf("工具轮后应再请求一次，实际 %d 次", client.callCount())
	}
	if executor.callCount() != 1 {
		t.Errorf("工具应被执行一次，实际 %d 次", executor.callCount())
	}

	// 第二次请求必须「先带 tool_calls 的 assistant，再接 tool 结果」
	second := client.callAt(t, 1)
	var got []string
	for _, message := range second.Messages {
		got = append(got, message.Role+":"+message.Content)
	}
	want := []string{"user:查一下状态", "assistant:我查一下", "tool:ok"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("工具轮消息顺序不符合预期: %v", got)
	}

	assistant := second.Messages[1]
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "c1" {
		t.Errorf("assistant 消息未带上 tool_calls: %+v", assistant)
	}
	if second.Messages[2].ToolCallID != "c1" {
		t.Errorf("工具结果未带上 ToolCallID: %+v", second.Messages[2])
	}
}

func TestAgentToolRoundLimit(t *testing.T) {
	call := llm.ToolCall{ID: "c1", Name: "loop_forever"}
	toolTurn := []llm.Event{{ToolCalls: []llm.ToolCall{call}}}

	client := &scriptedClient{turns: [][]llm.Event{toolTurn, toolTurn, toolTurn, toolTurn}}
	executor := &fakeExecutor{results: []llm.Message{{Role: llm.RoleTool, ToolCallID: "c1"}}}
	agent := newTestAgent(t, client, AgentConfig{
		ToolExecutor:  executor,
		MaxToolRounds: 3,
	})

	err := agent.Chat(context.Background(), "开始", func(string) error { return nil })
	if err == nil {
		t.Fatal("工具循环达到上限时应报错")
	}
	if !strings.Contains(err.Error(), "工具调用轮次超过上限 3") {
		t.Errorf("错误信息不符合预期: %v", err)
	}
	if executor.callCount() != 3 {
		t.Errorf("上限 3 时应执行 3 次工具，实际 %d 次", executor.callCount())
	}
}

func TestAgentSkipsEmptyAssistantText(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{{}}}
	agent := newTestAgent(t, client, AgentConfig{})

	collectText(t, agent, "在吗")

	got := historyStrings(agent)
	if len(got) != 1 || got[0] != "user:在吗" {
		t.Errorf("空助手文本不应写入历史: %v", got)
	}
}

func TestAgentDeduplicatesConsecutiveMessages(t *testing.T) {
	client := &scriptedClient{}
	agent := newTestAgent(t, client, AgentConfig{})

	agent.appendMessage(llm.Message{Role: llm.RoleUser, Content: "666"})
	agent.appendMessage(llm.Message{Role: llm.RoleUser, Content: "666"})
	agent.appendMessage(llm.Message{Role: llm.RoleUser, Content: "777"})

	got := historyStrings(agent)
	want := []string{"user:666", "user:777"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("连续重复消息未被去重: %v", got)
	}
}

func TestAgentRepeatedUserTextStillSentToModel(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("好")}}
	agent := newTestAgent(t, client, AgentConfig{})

	// 预置一条相同内容的历史，使本轮命中去重规则
	agent.appendMessage(llm.Message{Role: llm.RoleUser, Content: "666"})
	collectText(t, agent, "666")

	request := client.callAt(t, 0)
	if len(request.Messages) != 2 {
		t.Fatalf("本轮用户消息必须发给模型，实际 messages 为 %d 条", len(request.Messages))
	}

	// 历史里被去重，只剩预置的那一条加助手回复
	got := historyStrings(agent)
	want := []string{"user:666", "assistant:好"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("历史去重行为不符合预期: %v", got)
	}
}

func TestAgentInterruptRewritesLastAssistant(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("你好呀")}}
	agent := newTestAgent(t, client, AgentConfig{})

	collectText(t, agent, "你好")
	agent.Interrupt("你好")

	got := historyStrings(agent)
	want := []string{"user:你好", "assistant:你好...", "user:" + interruptedMarker}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("打断改写不符合预期: %v", got)
	}
}

func TestAgentInterruptWithoutAssistantAppends(t *testing.T) {
	agent := newTestAgent(t, &scriptedClient{}, AgentConfig{})

	agent.Interrupt("喂")

	got := historyStrings(agent)
	want := []string{"assistant:喂...", "user:" + interruptedMarker}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("无助手消息时的打断行为不符合预期: %v", got)
	}
}

func TestAgentInterruptOnlyOncePerTurn(t *testing.T) {
	agent := newTestAgent(t, &scriptedClient{}, AgentConfig{})

	agent.Interrupt("第一次")
	agent.Interrupt("第二次")

	got := historyStrings(agent)
	want := []string{"assistant:第一次...", "user:" + interruptedMarker}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("打断每轮只应生效一次: %v", got)
	}
}

func TestAgentInterruptResetsOnNextChat(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("好")}}
	agent := newTestAgent(t, client, AgentConfig{})

	agent.Interrupt("先来一次")
	collectText(t, agent, "第二轮")
	agent.Interrupt("再来一次")

	got := historyStrings(agent)
	want := []string{
		"assistant:先来一次...",
		"user:" + interruptedMarker,
		"user:第二轮",
		// 第二次打断改写的是本轮刚生成的助手消息，而不是再追加一条
		"assistant:再来一次...",
		"user:" + interruptedMarker,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("新一轮应重置打断标记: %v", got)
	}
}

func TestAgentInterruptAsSystemMode(t *testing.T) {
	client := &scriptedClient{}
	agent := newTestAgent(t, client, AgentConfig{InterruptMode: InterruptAsSystem})

	if strings.Contains(agent.System(), interruptedMarker) {
		t.Error("system 模式不应把打断说明追加进系统提示词")
	}

	agent.Interrupt("喂")

	got := historyStrings(agent)
	want := []string{"assistant:喂...", "system:" + interruptedMarker}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("system 模式打断标记角色不符合预期: %v", got)
	}
}

func TestAgentUserModeAppendsInterruptNotice(t *testing.T) {
	agent := newTestAgent(t, &scriptedClient{}, AgentConfig{System: "你是 Mili"})

	system := agent.System()
	if !strings.HasPrefix(system, "你是 Mili") {
		t.Errorf("系统提示词前缀丢失: %q", system)
	}
	if !strings.Contains(system, interruptNotice) {
		t.Errorf("user 模式应追加打断说明: %q", system)
	}
}

func TestAgentDefaultSystem(t *testing.T) {
	agent := newTestAgent(t, &scriptedClient{}, AgentConfig{})

	if !strings.HasPrefix(agent.System(), defaultSystem) {
		t.Errorf("未配置系统提示词时应使用兜底值: %q", agent.System())
	}
}

func TestAgentStopsWhenCallbackFails(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("一", "二")}}
	agent := newTestAgent(t, client, AgentConfig{})

	sentinel := errors.New("调用方主动停止")
	err := agent.Chat(context.Background(), "喂", func(string) error { return sentinel })

	if !errors.Is(err, sentinel) {
		t.Fatalf("期望原样返回回调错误，实际: %v", err)
	}

	got := historyStrings(agent)
	if len(got) != 1 || got[0] != "user:喂" {
		t.Errorf("中止时不应写入助手文本: %v", got)
	}
}

func TestAgentToolCallWithoutExecutor(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "query_status"}}}},
	}}
	agent := newTestAgent(t, client, AgentConfig{Tools: []llm.Tool{{Name: "query_status"}}})

	err := agent.Chat(context.Background(), "查一下", func(string) error { return nil })
	if err == nil {
		t.Fatal("未配置 ToolExecutor 时收到工具调用应报错")
	}
	if !strings.Contains(err.Error(), "未配置 ToolExecutor") {
		t.Errorf("错误信息不符合预期: %v", err)
	}
}

func TestAgentToolExecutorErrorPropagates(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "boom"}}}},
	}}
	executor := &fakeExecutor{err: errors.New("工具炸了")}
	agent := newTestAgent(t, client, AgentConfig{ToolExecutor: executor})

	err := agent.Chat(context.Background(), "查一下", func(string) error { return nil })
	if err == nil {
		t.Fatal("工具执行失败时应报错")
	}
	if !strings.Contains(err.Error(), "执行工具失败") || !errors.Is(err, executor.err) {
		t.Errorf("错误未包装根因: %v", err)
	}
}

func TestAgentLLMErrorPropagates(t *testing.T) {
	sentinel := errors.New("端点 500")
	client := &scriptedClient{err: sentinel}
	agent := newTestAgent(t, client, AgentConfig{})

	err := agent.Chat(context.Background(), "喂", func(string) error { return nil })
	if !errors.Is(err, sentinel) {
		t.Fatalf("LLM 错误应原样上抛，实际: %v", err)
	}
}

func TestAgentHistoryReturnsCopy(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("好")}}
	agent := newTestAgent(t, client, AgentConfig{})

	collectText(t, agent, "你好")

	snapshot := agent.History()
	snapshot[0].Content = "被改掉了"

	if agent.History()[0].Content != "你好" {
		t.Error("History() 应返回副本，外部修改不应影响内部状态")
	}
}

func TestAgentReset(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("好")}}
	agent := newTestAgent(t, client, AgentConfig{})

	collectText(t, agent, "你好")
	agent.Reset()

	if len(agent.History()) != 0 {
		t.Errorf("Reset 后历史应为空: %v", historyStrings(agent))
	}
}

func TestAgentChatRejectsNilCallback(t *testing.T) {
	agent := newTestAgent(t, &scriptedClient{}, AgentConfig{})

	if err := agent.Chat(context.Background(), "喂", nil); err == nil {
		t.Error("onText 为空时应报错")
	}
}
