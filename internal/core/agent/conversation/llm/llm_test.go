package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEndpoint 是一个返回预置 SSE 分片的假 OpenAI 兼容端点。
type fakeEndpoint struct {
	*httptest.Server

	mu       sync.Mutex
	requests []map[string]any
}

// newFakeEndpoint 启动假端点；chunks 按顺序以 data: 行写出，末尾补 [DONE]。
func newFakeEndpoint(t *testing.T, chunks ...string) *fakeEndpoint {
	t.Helper()

	endpoint := &fakeEndpoint{}
	endpoint.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)

		endpoint.mu.Lock()
		endpoint.requests = append(endpoint.requests, payload)
		endpoint.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))

	t.Cleanup(endpoint.Close)
	return endpoint
}

// lastRequest 返回最近一次收到的请求体。
func (e *fakeEndpoint) lastRequest(t *testing.T) map[string]any {
	t.Helper()

	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.requests) == 0 {
		t.Fatal("假端点没有收到任何请求")
	}
	return e.requests[len(e.requests)-1]
}

// newClient 针对假端点构造客户端。
func newClient(t *testing.T, endpoint *fakeEndpoint) *Client {
	t.Helper()

	client, err := New(Config{
		BaseURL: endpoint.URL + "/v1",
		APIKey:  "test-key",
		Model:   "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	return client
}

// collect 收集全部事件；流出错时直接判定测试失败。
func collect(t *testing.T, client *Client, req Request) []Event {
	t.Helper()

	var events []Event
	err := client.ChatStream(context.Background(), req, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream 返回错误: %v", err)
	}
	return events
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("序列化测试分片失败: %v", err)
	}
	return string(raw)
}

// textChunk 构造一个只含文本增量的分片。
func textChunk(t *testing.T, text string) string {
	t.Helper()

	return mustJSON(t, map[string]any{
		"id":      "chunk",
		"object":  "chat.completion.chunk",
		"created": 1,
		"model":   "deepseek-chat",
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"content": text},
			"finish_reason": nil,
		}},
	})
}

// toolCallChunk 构造一个含工具调用分片的分片。
func toolCallChunk(t *testing.T, calls ...map[string]any) string {
	t.Helper()

	return mustJSON(t, map[string]any{
		"id":      "chunk",
		"object":  "chat.completion.chunk",
		"created": 1,
		"model":   "deepseek-chat",
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"tool_calls": calls},
			"finish_reason": nil,
		}},
	})
}

// toolCallFragment 构造一个工具调用分片；index 为负表示不下发 index 字段。
func toolCallFragment(index int, id, name, arguments string) map[string]any {
	function := map[string]any{}
	if name != "" {
		function["name"] = name
	}
	if arguments != "" {
		function["arguments"] = arguments
	}

	call := map[string]any{"function": function}
	if index >= 0 {
		call["index"] = index
	}
	if id != "" {
		call["id"] = id
		call["type"] = "function"
	}

	return call
}

func TestChatStreamTextChunks(t *testing.T) {
	endpoint := newFakeEndpoint(t, textChunk(t, "你"), textChunk(t, "好"))
	client := newClient(t, endpoint)

	events := collect(t, client, Request{
		Messages: []Message{{Role: RoleUser, Content: "在吗"}},
	})

	if len(events) != 2 {
		t.Fatalf("期望 2 个事件，实际 %d 个: %+v", len(events), events)
	}
	if events[0].Text != "你" || events[1].Text != "好" {
		t.Errorf("文本增量不符合预期: %+v", events)
	}
	for _, event := range events {
		if event.Text != "" && len(event.ToolCalls) > 0 {
			t.Errorf("同一个事件不应同时带文本与工具调用: %+v", event)
		}
	}
}

func TestChatStreamAccumulatesToolCallFragments(t *testing.T) {
	endpoint := newFakeEndpoint(t,
		toolCallChunk(t, toolCallFragment(0, "call_1", "query_status", "")),
		toolCallChunk(t, toolCallFragment(0, "", "", `{"room"`)),
		toolCallChunk(t, toolCallFragment(0, "", "", `:1}`)),
	)
	client := newClient(t, endpoint)

	events := collect(t, client, Request{
		Messages: []Message{{Role: RoleUser, Content: "查一下状态"}},
		Tools:    []Tool{{Name: "query_status", Description: "查询当前状态"}},
	})

	if len(events) != 1 {
		t.Fatalf("期望 1 个工具调用事件，实际 %d 个: %+v", len(events), events)
	}
	calls := events[0].ToolCalls
	if len(calls) != 1 {
		t.Fatalf("期望 1 个工具调用，实际 %d 个", len(calls))
	}
	if calls[0].ID != "call_1" {
		t.Errorf("工具调用 ID 不符合预期: %q", calls[0].ID)
	}
	if calls[0].Name != "query_status" {
		t.Errorf("工具名不符合预期: %q", calls[0].Name)
	}
	if calls[0].Arguments != `{"room":1}` {
		t.Errorf("参数分片未正确拼接: %q", calls[0].Arguments)
	}
}

func TestChatStreamParallelToolCalls(t *testing.T) {
	endpoint := newFakeEndpoint(t,
		toolCallChunk(t,
			toolCallFragment(0, "call_a", "tool_a", ""),
			toolCallFragment(1, "call_b", "tool_b", ""),
		),
		toolCallChunk(t, toolCallFragment(1, "", "", `{"b":2}`)),
		toolCallChunk(t, toolCallFragment(0, "", "", `{"a":1}`)),
	)
	client := newClient(t, endpoint)

	events := collect(t, client, Request{
		Messages: []Message{{Role: RoleUser, Content: "并行调用"}},
	})

	if len(events) != 1 {
		t.Fatalf("期望 1 个工具调用事件，实际 %d 个", len(events))
	}
	calls := events[0].ToolCalls
	if len(calls) != 2 {
		t.Fatalf("期望 2 个工具调用，实际 %d 个: %+v", len(calls), calls)
	}
	if calls[0].ID != "call_a" || calls[1].ID != "call_b" {
		t.Errorf("工具调用顺序未按首次出现排列: %+v", calls)
	}
	if calls[0].Arguments != `{"a":1}` {
		t.Errorf("call_a 参数不符合预期: %q", calls[0].Arguments)
	}
	if calls[1].Arguments != `{"b":2}` {
		t.Errorf("call_b 参数不符合预期: %q", calls[1].Arguments)
	}
}

func TestChatStreamToolCallWithoutIndex(t *testing.T) {
	endpoint := newFakeEndpoint(t,
		toolCallChunk(t, toolCallFragment(-1, "call_x", "tool_x", "")),
		toolCallChunk(t, toolCallFragment(-1, "", "", `{"k":1}`)),
	)
	client := newClient(t, endpoint)

	events := collect(t, client, Request{
		Messages: []Message{{Role: RoleUser, Content: "无 index 端点"}},
	})

	if len(events) != 1 || len(events[0].ToolCalls) != 1 {
		t.Fatalf("期望归并为 1 个工具调用，实际: %+v", events)
	}
	if got := events[0].ToolCalls[0].Arguments; got != `{"k":1}` {
		t.Errorf("按位置兜底归并失败，参数为 %q", got)
	}
}

func TestChatStreamReturnsErrorOnHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom","type":"server_error"}}`))
	}))
	t.Cleanup(server.Close)

	client, err := New(Config{BaseURL: server.URL + "/v1", APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}

	var events []Event
	err = client.ChatStream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(event Event) error {
		events = append(events, event)
		return nil
	})

	if err == nil {
		t.Fatal("端点返回 500 时应返回错误")
	}
	if len(events) != 0 {
		t.Errorf("失败时不应回调任何事件，实际收到 %d 个", len(events))
	}
	if !strings.Contains(err.Error(), "发起流式请求失败") {
		t.Errorf("错误未带上操作上下文: %v", err)
	}
}

func TestChatStreamStopsOnCallbackError(t *testing.T) {
	endpoint := newFakeEndpoint(t, textChunk(t, "a"), textChunk(t, "b"))
	client := newClient(t, endpoint)

	sentinel := errors.New("调用方主动停止")
	var received []string

	err := client.ChatStream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(event Event) error {
		received = append(received, event.Text)
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("期望原样返回回调错误，实际: %v", err)
	}
	if len(received) != 1 {
		t.Errorf("中止后不应继续回调，实际收到 %v", received)
	}
}

func TestChatStreamContextCancelled(t *testing.T) {
	// 分片在测试协程内预先生成，避免在 HTTP 处理协程里碰 testing.T。
	firstChunk := textChunk(t, "开始")
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if _, err := fmt.Fprintf(w, "data: %s\n\n", firstChunk); err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	client, err := New(Config{BaseURL: server.URL + "/v1", APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	count := 0
	done := make(chan error, 1)
	go func() {
		done <- client.ChatStream(ctx, Request{
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, func(Event) error {
			count++
			cancel() // 收到首片后立即取消
			return nil
		})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ctx 取消后应返回错误")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 取消后 ChatStream 未及时返回")
	}

	if count != 1 {
		t.Errorf("取消前应只收到首片，实际收到 %d 片", count)
	}
}

func TestSystemPromptInsertedFirst(t *testing.T) {
	endpoint := newFakeEndpoint(t, textChunk(t, "hi"))
	client := newClient(t, endpoint)

	collect(t, client, Request{
		System:   "你是 Mili",
		Messages: []Message{{Role: RoleUser, Content: "你好"}},
	})

	messages, ok := endpoint.lastRequest(t)["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages 字段不符合预期: %+v", endpoint.lastRequest(t)["messages"])
	}

	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "你是 Mili" {
		t.Errorf("系统提示词未置于首位: %+v", first)
	}

	second, _ := messages[1].(map[string]any)
	if second["role"] != "user" || second["content"] != "你好" {
		t.Errorf("用户消息不符合预期: %+v", second)
	}
}

func TestToolsSerializedOnlyWhenProvided(t *testing.T) {
	endpoint := newFakeEndpoint(t, textChunk(t, "hi"))
	client := newClient(t, endpoint)

	collect(t, client, Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if _, exists := endpoint.lastRequest(t)["tools"]; exists {
		t.Error("未声明工具时不应下发 tools 字段")
	}

	collect(t, client, Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{{
			Name:        "query_status",
			Description: "查询当前状态",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"room":{"type":"string"}}}`),
		}},
	})

	tools, ok := endpoint.lastRequest(t)["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools 字段不符合预期: %+v", endpoint.lastRequest(t)["tools"])
	}

	tool, _ := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("工具类型应为 function，实际 %v", tool["type"])
	}

	function, _ := tool["function"].(map[string]any)
	if function["name"] != "query_status" || function["description"] != "查询当前状态" {
		t.Errorf("工具描述不符合预期: %+v", function)
	}
	if _, exists := function["parameters"]; !exists {
		t.Error("工具应下发 parameters schema")
	}
}

func TestToolWithoutParametersGetsEmptySchema(t *testing.T) {
	endpoint := newFakeEndpoint(t, textChunk(t, "hi"))
	client := newClient(t, endpoint)

	collect(t, client, Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{{Name: "no_args"}},
	})

	tools, _ := endpoint.lastRequest(t)["tools"].([]any)
	tool, _ := tools[0].(map[string]any)
	function, _ := tool["function"].(map[string]any)

	parameters, _ := function["parameters"].(map[string]any)
	if parameters["type"] != "object" {
		t.Errorf("无参数工具应下发空对象 schema，实际: %+v", function["parameters"])
	}
}

func TestTemperatureDefaultsAndOverride(t *testing.T) {
	endpoint := newFakeEndpoint(t, textChunk(t, "hi"))

	def := newClient(t, endpoint)
	collect(t, def, Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if got := endpoint.lastRequest(t)["temperature"]; got != 1.0 {
		t.Errorf("未配置温度时应为默认值 1.0，实际 %v", got)
	}

	warm, err := New(Config{
		BaseURL:     endpoint.URL + "/v1",
		APIKey:      "k",
		Model:       "m",
		Temperature: 0.7,
	})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	collect(t, warm, Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if got := endpoint.lastRequest(t)["temperature"]; got != 0.7 {
		t.Errorf("温度覆盖未生效，实际 %v", got)
	}
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	if _, err := New(Config{Model: "m"}); err == nil {
		t.Error("缺少 base_url 时应报错")
	}
	if _, err := New(Config{BaseURL: "https://example.com/v1"}); err == nil {
		t.Error("缺少 model 时应报错")
	}
}

func TestChatStreamRejectsNilCallback(t *testing.T) {
	client, err := New(Config{BaseURL: "https://example.com/v1", Model: "m"})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}

	if err := client.ChatStream(context.Background(), Request{}, nil); err == nil {
		t.Error("onEvent 为空时应报错")
	}
}

func TestModelAccessor(t *testing.T) {
	client, err := New(Config{BaseURL: "https://example.com/v1", Model: "deepseek-chat"})
	if err != nil {
		t.Fatalf("构造客户端失败: %v", err)
	}
	if client.Model() != "deepseek-chat" {
		t.Errorf("Model() 返回 %q", client.Model())
	}
}
