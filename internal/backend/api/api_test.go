package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/backend/server"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation/llm"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/memory"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/tool"
	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/upload"
)

// newRequest 构造带路径值与请求体的请求。
func newRequest(method, path, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}

	return req
}

func TestSpeakEnqueuesTrimmedTextWithEmotion(t *testing.T) {
	var gotText, gotEmotion string

	handler := &Handler{Speak: func(text, emotion string) error {
		gotText, gotEmotion = text, emotion
		return nil
	}}

	recorder := httptest.NewRecorder()
	handler.handleSpeak(recorder, newRequest(http.MethodPost, "/api/speak", `{"text":"  说话  ","emotion":"joy"}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}
	if gotText != "说话" || gotEmotion != "joy" {
		t.Fatalf("入队参数 = (%q, %q), want (说话, joy)", gotText, gotEmotion)
	}
}

// 未装配播报、空文本、坏 JSON、错方法、队列满：都必须给出明确状态码而不是 200。
func TestSpeakRejections(t *testing.T) {
	pass := func(string, string) error { return nil }
	reject := func(string, string) error { return errors.New("播报队列已满") }

	cases := []struct {
		name   string
		speak  func(string, string) error
		method string
		body   string
		want   int
	}{
		{name: "未装配播报", speak: nil, method: http.MethodPost, body: `{"text":"你好"}`, want: http.StatusServiceUnavailable},
		{name: "空文本", speak: pass, method: http.MethodPost, body: `{"text":"   "}`, want: http.StatusBadRequest},
		{name: "坏 JSON", speak: pass, method: http.MethodPost, body: `{`, want: http.StatusBadRequest},
		{name: "错方法", speak: pass, method: http.MethodGet, want: http.StatusMethodNotAllowed},
		{name: "队列满", speak: reject, method: http.MethodPost, body: `{"text":"你好"}`, want: http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &Handler{Speak: tc.speak}

			recorder := httptest.NewRecorder()
			handler.handleSpeak(recorder, newRequest(tc.method, "/api/speak", tc.body))

			if recorder.Code != tc.want {
				t.Fatalf("状态码 = %d, want %d", recorder.Code, tc.want)
			}
		})
	}
}

// 接入客户端路径可以配成 "/" 兜住所有路径，/api/* 必须仍然落到接口层，
// 否则请求会被当成 WebSocket 握手（405/426）而不是拿到 JSON 响应。
func TestRoutesTakePrecedenceOverCatchAllClientPath(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Clients = []config.ClientConfig{{Platform: "qq", AdapterKey: "onebot_v11", Path: "/"}}

	called := 0
	handler := &Handler{
		Config: func() (*config.Config, error) { return cfg, nil },
		Speak:  func(string, string) error { called++; return nil },
	}

	srv := server.ProvideServer(cfg, handler.Routes()...)

	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, newRequest(http.MethodPost, "/api/speak", `{"text":"你好"}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("/api/speak 状态码 = %d, want 200（被接入客户端的兜底路径吃掉了）", recorder.Code)
	}
	if called != 1 {
		t.Fatalf("播报入队次数 = %d, want 1", called)
	}

	// 旧路径已下线：请求会落到兜底的接入端处理上，而不是接口层
	legacy := httptest.NewRecorder()
	srv.Handler.ServeHTTP(legacy, newRequest(http.MethodPost, "/inject", `{"text":"你好"}`))
	if legacy.Code == http.StatusOK {
		t.Fatal("/inject 仍在响应，路径切换没做完")
	}
}

// 配置接口只回「有没有配」，明文凭据绝不能出现在响应体里。
func TestConfigRedactsSecrets(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.APIKey = "sk-绝密值"
	cfg.LLM.BaseURL = "https://api.example.com/v1"
	cfg.TTS.OpenAI.APIKey = "" // 未配置：要能区分出来
	cfg.Stream.Cookie = "SESSDATA=绝密cookie"
	cfg.Memory.QueueWaitTimeout = config.Duration(5 * time.Minute)

	handler := &Handler{Config: func() (*config.Config, error) { return cfg, nil }}

	recorder := httptest.NewRecorder()
	handler.handleConfig(recorder, newRequest(http.MethodGet, "/api/config", ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	body := recorder.Body.String()
	for _, leaked := range []string{"sk-绝密值", "SESSDATA=绝密cookie"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("响应体里出现了明文凭据 %q", leaked)
		}
	}
	for _, want := range []string{
		`"api_key":{"configured":true}`,  // 已配置的密钥
		`"api_key":{"configured":false}`, // 未配置的密钥
		`"cookie":{"configured":true}`,
		`"base_url":"https://api.example.com/v1"`,
		`"queue_wait_timeout":"5m0s"`, // 时间段要可读，不是纳秒整数
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("响应体里缺少 %s", want)
		}
	}

	// cookie_file 是路径不是凭据，不能被脱敏规则误伤
	var snapshot map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	streamSection, ok := snapshot["stream"].(map[string]any)
	if !ok {
		t.Fatalf("stream 分区缺失: %#v", snapshot["stream"])
	}
	if _, ok := streamSection["cookie_file"].(string); !ok {
		t.Fatalf("cookie_file 应为字符串，实际 %#v", streamSection["cookie_file"])
	}
}

// 未装配的能力必须回 503，而不是假装成功。
func TestUnconfiguredCapabilitiesReturn503(t *testing.T) {
	handler := &Handler{}

	cases := []struct {
		name string
		path string
		call func(*httptest.ResponseRecorder)
	}{
		{name: "配置不可用", path: "/api/config", call: func(rec *httptest.ResponseRecorder) {
			handler.handleConfig(rec, newRequest(http.MethodGet, "/api/config", ""))
		}},
		{name: "会话未启用", path: "/api/sessions", call: func(rec *httptest.ResponseRecorder) {
			handler.handleSessions(rec, newRequest(http.MethodGet, "/api/sessions", ""))
		}},
		{name: "会话历史不可用", path: "/api/sessions/room_1/history", call: func(rec *httptest.ResponseRecorder) {
			req := newRequest(http.MethodGet, "/api/sessions/room_1/history", "")
			req.SetPathValue("id", "room_1")
			handler.handleHistory(rec, req)
		}},
		{name: "发消息不可用", path: "/api/sessions/room_1/messages", call: func(rec *httptest.ResponseRecorder) {
			req := newRequest(http.MethodPost, "/api/sessions/room_1/messages", `{"text":"你好"}`)
			req.SetPathValue("id", "room_1")
			handler.handleSendMessage(rec, req)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tc.call(recorder)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s 状态码 = %d, want 503", tc.path, recorder.Code)
			}
		})
	}
}

// 状态接口只做聚合：注入什么就回什么，字段名用 snake_case。
func TestStatusAggregatesInjectedSources(t *testing.T) {
	handler := &Handler{
		Started: time.Now().Add(-90 * time.Second),
		Clients: func() []string { return []string{"bilibili", "qq"} },
		Upload:  func() upload.PipelineStats { return upload.PipelineStats{QueueLen: 3, QueueSize: 256, Dropped: 7} },
	}

	recorder := httptest.NewRecorder()
	handler.handleStatus(recorder, newRequest(http.MethodGet, "/api/status", ""))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", recorder.Code)
	}

	var status map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("解析响应: %v", err)
	}

	clients, ok := status["clients"].(map[string]any)
	if !ok || clients["count"].(float64) != 2 {
		t.Fatalf("clients = %#v, want count=2", status["clients"])
	}

	pipeline, ok := status["upload"].(map[string]any)
	if !ok || pipeline["queue_len"].(float64) != 3 || pipeline["dropped"].(float64) != 7 {
		t.Fatalf("upload = %#v, want queue_len=3 dropped=7", status["upload"])
	}

	// 没装配播报与会话时不出现对应字段，而不是回 0 让人误以为可用
	if _, exists := status["broadcast"]; exists {
		t.Fatal("未装配播报时不该有 broadcast 字段")
	}
	if _, exists := status["sessions"]; exists {
		t.Fatal("会话未启用时不该有 sessions 字段")
	}
}

// 渠道号前缀与 [[clients]] 的适配器要对得上，否则拒绝而不是造一个幽灵会话。
func TestChannelBinding(t *testing.T) {
	cfg := &config.Config{}
	cfg.Clients = []config.ClientConfig{{Platform: "qq-main", AdapterKey: "onebot_v11"}}

	cases := []struct {
		name        string
		channelID   string
		wantOK      bool
		wantPlat    string
		wantChannel string
	}{
		{name: "群消息", channelID: "group_10001", wantOK: true, wantPlat: "qq-main", wantChannel: "group"},
		{name: "直播间未配置", channelID: "room_123", wantOK: false},
		{name: "未知前缀", channelID: "weibo_1", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			platform, channelType, ok := channelBinding(cfg, tc.channelID)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if platform != tc.wantPlat || channelType != tc.wantChannel {
				t.Fatalf("binding = (%q, %q), want (%q, %q)", platform, channelType, tc.wantPlat, tc.wantChannel)
			}
		})
	}
}

// 记忆接口：追加、按关键词召回、最近快照、墓碑删除。
func TestMemoryEndpoints(t *testing.T) {
	store, err := memory.Open(filepath.Join(t.TempDir(), "memory.jsonl"))
	if err != nil {
		t.Fatalf("打开记忆: %v", err)
	}
	defer store.Close()

	handler := &Handler{Memory: store}

	// 追加
	recorder := httptest.NewRecorder()
	handler.handleMemoryAdd(recorder, newRequest(http.MethodPost, "/api/memory", `{"text":"  记得买牛奶  ","user":"观众"}`))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("追加状态码 = %d, want 201", recorder.Code)
	}
	var added struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &added); err != nil {
		t.Fatalf("解析追加响应: %v", err)
	}
	if added.ID != 1 {
		t.Fatalf("第一条记忆的 ID = %d, want 1", added.ID)
	}

	// 最近快照（不带 query）
	recorder = httptest.NewRecorder()
	handler.handleMemoryList(recorder, newRequest(http.MethodGet, "/api/memory", ""))
	if !strings.Contains(recorder.Body.String(), "记得买牛奶") {
		t.Fatalf("快照里没有刚写入的记忆: %s", recorder.Body.String())
	}

	// 按关键词召回
	recorder = httptest.NewRecorder()
	handler.handleMemoryList(recorder, newRequest(http.MethodGet, "/api/memory?query=牛奶", ""))
	if !strings.Contains(recorder.Body.String(), "记得买牛奶") {
		t.Fatalf("召回没有命中: %s", recorder.Body.String())
	}

	// 空文本与非法 ID
	recorder = httptest.NewRecorder()
	handler.handleMemoryAdd(recorder, newRequest(http.MethodPost, "/api/memory", `{"text":"   "}`))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("空文本状态码 = %d, want 400", recorder.Code)
	}

	// 墓碑删除
	recorder = httptest.NewRecorder()
	deleteReq := newRequest(http.MethodDelete, "/api/memory/1", "")
	deleteReq.SetPathValue("id", "1")
	handler.handleMemoryDelete(recorder, deleteReq)
	if recorder.Code != http.StatusOK {
		t.Fatalf("删除状态码 = %d, want 200", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	handler.handleMemoryList(recorder, newRequest(http.MethodGet, "/api/memory", ""))
	if strings.Contains(recorder.Body.String(), "记得买牛奶") {
		t.Fatalf("删除后仍能列出该条: %s", recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	deleteReq = newRequest(http.MethodDelete, "/api/memory/1", "")
	deleteReq.SetPathValue("id", "1")
	handler.handleMemoryDelete(recorder, deleteReq)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("重复删除状态码 = %d, want 404", recorder.Code)
	}

	// 未配置记忆
	empty := &Handler{}
	recorder = httptest.NewRecorder()
	empty.handleMemoryList(recorder, newRequest(http.MethodGet, "/api/memory", ""))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("未配置记忆状态码 = %d, want 503", recorder.Code)
	}
}

// 工具接口：列清单，且只放行只读工具的直接调用。
func TestToolEndpoints(t *testing.T) {
	registry := tool.New()
	noop := func(context.Context, json.RawMessage) (string, error) { return "结果", nil }

	if err := registry.RegisterReadOnly(llm.Tool{Name: "peek", Description: "只读工具"}, noop); err != nil {
		t.Fatalf("注册只读工具: %v", err)
	}
	if err := registry.Register(llm.Tool{Name: "write", Description: "有副作用的工具"}, noop); err != nil {
		t.Fatalf("注册有副作用的工具: %v", err)
	}

	handler := &Handler{Tools: registry}

	// 清单：两个工具，只读标记要如实反映
	recorder := httptest.NewRecorder()
	handler.handleToolsList(recorder, newRequest(http.MethodGet, "/api/tools", ""))
	body := recorder.Body.String()
	if !strings.Contains(body, `"read_only":true`) || !strings.Contains(body, `"read_only":false`) {
		t.Fatalf("工具清单的只读标记不对: %s", body)
	}

	// 只读工具可直接调用
	recorder = httptest.NewRecorder()
	callReq := newRequest(http.MethodPost, "/api/tools/peek", `{"args":{"query":"x"}}`)
	callReq.SetPathValue("name", "peek")
	handler.handleToolCall(recorder, callReq)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "结果") {
		t.Fatalf("只读工具调用 = %d %s", recorder.Code, recorder.Body.String())
	}

	// 有副作用的工具必须被拒
	recorder = httptest.NewRecorder()
	callReq = newRequest(http.MethodPost, "/api/tools/write", `{"args":{}}`)
	callReq.SetPathValue("name", "write")
	handler.handleToolCall(recorder, callReq)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("有副作用的工具状态码 = %d, want 403", recorder.Code)
	}

	// 未注册的工具
	recorder = httptest.NewRecorder()
	callReq = newRequest(http.MethodPost, "/api/tools/nope", `{}`)
	callReq.SetPathValue("name", "nope")
	handler.handleToolCall(recorder, callReq)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("未注册工具状态码 = %d, want 404", recorder.Code)
	}
}
