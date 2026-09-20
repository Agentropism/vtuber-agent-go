package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agentropism/vtuber-agent-go/internal/config"
)

func TestHandleInjectEnqueuesTrimmedTextWithEmotion(t *testing.T) {
	var got InjectRequest
	SetInjectHandler(func(req InjectRequest) error {
		got = req
		return nil
	})
	t.Cleanup(func() { SetInjectHandler(nil) })

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"text":"  测试注入  ","emotion":"joy"}`)
	handleInject(rec, httptest.NewRequest(http.MethodPost, "/inject", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200，响应体 %s", rec.Code, rec.Body.String())
	}
	if got.Text != "测试注入" {
		t.Fatalf("文本 = %q, want %q（首尾空白应被裁掉）", got.Text, "测试注入")
	}
	if got.Emotion != "joy" {
		t.Fatalf("表情 = %q, want %q", got.Emotion, "joy")
	}
}

func TestHandleInjectRejections(t *testing.T) {
	pass := func(InjectRequest) error { return nil }
	reject := func(InjectRequest) error { return errors.New("播报队列已满") }

	cases := []struct {
		name       string
		method     string
		body       string
		handler    InjectFunc
		wantStatus int
	}{
		{"非 POST 方法", http.MethodGet, `{"text":"x"}`, pass, http.StatusMethodNotAllowed},
		{"空文本", http.MethodPost, `{"text":"   "}`, pass, http.StatusBadRequest},
		{"非法 JSON", http.MethodPost, `{`, pass, http.StatusBadRequest},
		{"未装配播报", http.MethodPost, `{"text":"x"}`, nil, http.StatusServiceUnavailable},
		{"队列拒收", http.MethodPost, `{"text":"x"}`, reject, http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SetInjectHandler(tc.handler)
			t.Cleanup(func() { SetInjectHandler(nil) })

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, "/inject", strings.NewReader(tc.body))
			handleInject(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("状态码 = %d, want %d，响应体 %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

// 客户端路径可以配成 "/" 并兜住所有路径，/inject 必须显式注册才能落到自己的处理上，
// 否则会被当成 WebSocket 握手而返回 405。
func TestProvideServerRoutesInjectBeforeCatchAllClientPath(t *testing.T) {
	SetInjectHandler(func(InjectRequest) error { return nil })
	t.Cleanup(func() { SetInjectHandler(nil) })

	cfg := &config.Config{}
	cfg.Clients = []config.ClientConfig{{Platform: "qq", AdapterKey: "onebot_v11", Path: "/"}}

	srv := ProvideServer(cfg)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"text":"测试"}`)
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/inject", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200（/inject 被 WebSocket 路由吃掉了）", rec.Code)
	}
}
