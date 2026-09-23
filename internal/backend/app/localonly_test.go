package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 非本机地址一律拒绝，回环放行。
func TestLocalOnlyGuard(t *testing.T) {
	handler := localOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	denied := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/", nil)
	req.RemoteAddr = "192.168.1.20:54321"
	handler.ServeHTTP(denied, req)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("非本机状态码 = %d, want 403", denied.Code)
	}

	allowed := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/debug/", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	handler.ServeHTTP(allowed, req)
	if allowed.Code != http.StatusOK {
		t.Fatalf("本机状态码 = %d, want 200", allowed.Code)
	}
}

// 回环地址之外的判定：常见内网地址与解析不了的输入都不是本机。
func TestIsLocalRequest(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:1234": true,
		"[::1]:1234":     true,
		"192.168.1.5:80": false,
		"10.0.0.7:80":    false,
		"garbage":        false,
	}

	for addr, want := range cases {
		if got := isLocalRequest(addr); got != want {
			t.Fatalf("isLocalRequest(%q) = %v, want %v", addr, got, want)
		}
	}
}

// WSL 宿主机的地址（默认路由网关）也算本机：Windows 浏览器就是从这个地址来的。
func TestIsLocalRequestAllowsHostGateway(t *testing.T) {
	gateway := readDefaultGateway()
	if gateway == nil {
		t.Skip("当前环境没有默认路由网关")
	}
	if !isLocalRequest(net.JoinHostPort(gateway.String(), "54321")) {
		t.Fatalf("宿主机地址 %s 应当被放行", gateway)
	}
}
