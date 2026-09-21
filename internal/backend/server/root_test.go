package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
)

// 页面挂在根路径，而接入客户端也可能把 [[clients]].path 配成 "/"。
// ServeMux 不允许注册两次 "/"，所以这时必须合成分流：升级请求给接入端、其余给页面。
func TestRootPathMergesPageAndCatchAllClient(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Clients = []config.ClientConfig{{Platform: "qq", AdapterKey: "onebot_v11", Path: "/"}}

	pageCalled := false
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("页面"))
	})

	srv := ProvideServer(cfg, Route{Pattern: "/", Handler: page})

	// 普通请求：页面
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if !pageCalled || recorder.Code != http.StatusOK {
		t.Fatalf("普通请求没有落到页面：called=%v code=%d", pageCalled, recorder.Code)
	}

	// WebSocket 升级请求：接入端（这里没有完整握手头，Accept 会失败，但绝不能落到页面）
	pageCalled = false
	upgrade := httptest.NewRequest(http.MethodGet, "/", nil)
	upgrade.Header.Set("Upgrade", "websocket")
	recorder = httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, upgrade)

	if pageCalled {
		t.Fatal("WebSocket 升级请求被页面处理了，接入端会连不上")
	}
	if recorder.Code == http.StatusOK {
		t.Fatalf("升级请求不该返回 200：%d", recorder.Code)
	}
}

// 更具体的 pattern 仍然优先：接入路径配成 "/" 不该吃掉 /favicon.ico 之类的额外路由。
func TestExtraRouteBeatsCatchAllClient(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Clients = []config.ClientConfig{{Platform: "qq", AdapterKey: "onebot_v11", Path: "/"}}

	extraCalled := false
	extra := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extraCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	srv := ProvideServer(cfg, Route{Pattern: "/favicon.ico", Handler: extra})

	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))

	if !extraCalled || recorder.Code != http.StatusNoContent {
		t.Fatalf("额外路由被接入路径吃掉了：called=%v code=%d", extraCalled, recorder.Code)
	}
}
