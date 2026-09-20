package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/config"
	"github.com/Agentropism/vtuber-agent-go/internal/stream"
)

// newTestLogin 造一个把 passport 接口指向假服务器的登录服务。
func newTestLogin(t *testing.T, apiURL, cookiePath string) *loginService {
	t.Helper()

	return &loginService{
		cfg: config.StreamConfig{CookieFile: cookiePath},
		api: stream.LoginConfig{BaseURL: apiURL},
	}
}

func serveLogin(t *testing.T, s *loginService) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	for _, route := range s.routes() {
		mux.Handle(route.Pattern, route.Handler)
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

// 登录页与它的两个端点只允许本机访问：能开播的凭据不该暴露给局域网。
func TestLoginRoutesRejectNonLoopback(t *testing.T) {
	s := newTestLogin(t, "http://127.0.0.1:1", filepath.Join(t.TempDir(), "cookie.txt"))

	for _, route := range s.routes() {
		req := httptest.NewRequest(http.MethodGet, route.Pattern, nil)
		req.RemoteAddr = "192.168.1.20:54321"
		rec := httptest.NewRecorder()

		route.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s 放行了非本机访问: %d", route.Pattern, rec.Code)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:1234": true,
		"[::1]:1234":     true,
		"192.168.1.5:80": false,
		"10.0.0.7:80":    false,
		"garbage":        false,
	}

	for addr, want := range cases {
		if got := isLoopback(addr); got != want {
			t.Fatalf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

// 登录页本身要能打开（页面上会去取二维码与轮询状态）。
func TestLoginPageServes(t *testing.T) {
	s := newTestLogin(t, "http://127.0.0.1:1", filepath.Join(t.TempDir(), "cookie.txt"))
	server := serveLogin(t, s)

	resp, err := http.Get(server.URL + loginPagePattern)
	if err != nil {
		t.Fatalf("取登录页: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}

	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "扫码") {
		t.Fatal("登录页里没有扫码说明")
	}
}

// 确认登录后：二维码是合法 PNG、状态是 confirmed、凭据按 0600 落盘。
func TestLoginConfirmedSavesCookie(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/generate"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://passport.example/h5","qrcode_key":"k1"}}`))
		case strings.HasSuffix(r.URL.Path, "/poll"):
			http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess"})
			http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "jct"})
			_, _ = w.Write([]byte(`{"code":0,"data":{"code":0,"message":"ok"}}`))
		default:
			t.Errorf("意外的路径: %s", r.URL.Path)
		}
	}))
	defer api.Close()

	cookiePath := filepath.Join(t.TempDir(), "cookie.txt")
	s := newTestLogin(t, api.URL, cookiePath)
	server := serveLogin(t, s)

	pngResp, err := http.Get(server.URL + loginQRCodePattern)
	if err != nil {
		t.Fatalf("取二维码: %v", err)
	}
	defer pngResp.Body.Close()

	if pngResp.StatusCode != http.StatusOK {
		t.Fatalf("二维码状态码 = %d", pngResp.StatusCode)
	}
	head := make([]byte, 8)
	if _, err := pngResp.Body.Read(head); err != nil {
		t.Fatalf("读二维码: %v", err)
	}
	if string(head[1:4]) != "PNG" {
		t.Fatalf("返回的不是 PNG: % x", head)
	}

	if status := getLoginStatus(t, server.URL+loginStatusPattern); status["state"] != string(stream.LoginConfirmed) {
		t.Fatalf("状态 = %v", status)
	}
	if s.Cookie() == "" {
		t.Fatal("确认登录后内存里应当有凭据")
	}

	saved, err := os.ReadFile(cookiePath)
	if err != nil {
		t.Fatalf("读落盘凭据: %v", err)
	}
	if !strings.Contains(string(saved), "SESSDATA=sess") || !strings.Contains(string(saved), "bili_jct=jct") {
		t.Fatalf("落盘凭据不对: %q", saved)
	}

	info, err := os.Stat(cookiePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("落盘权限 = %o, want 600", perm)
	}
}

// 过期要作废本地二维码并重新申请，而不是抱着死码一直轮询。
func TestLoginExpiredRegeneratesQRCode(t *testing.T) {
	generates := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/generate"):
			generates++
			_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://passport.example/h5","qrcode_key":"k` +
				strconv.Itoa(generates) + `"}}`))
		case strings.HasSuffix(r.URL.Path, "/poll"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"code":86038,"message":"expired"}}`))
		}
	}))
	defer api.Close()

	s := newTestLogin(t, api.URL, filepath.Join(t.TempDir(), "cookie.txt"))
	server := serveLogin(t, s)

	if status := getLoginStatus(t, server.URL+loginStatusPattern); status["state"] != string(stream.LoginExpired) {
		t.Fatalf("状态 = %v", status)
	}

	resp, err := http.Get(server.URL + loginQRCodePattern)
	if err != nil {
		t.Fatalf("重取二维码: %v", err)
	}
	defer resp.Body.Close()

	if generates != 2 {
		t.Fatalf("过期后应当重新申请二维码，实际申请次数 = %d", generates)
	}
}

func getLoginStatus(t *testing.T, url string) map[string]string {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("取状态: %v", err)
	}
	defer resp.Body.Close()

	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("解析状态: %v", err)
	}

	return payload
}

// newVerifyLogin 造一个推流状态固定的登录服务，用来测验证页。
func newVerifyLogin(t *testing.T, status streamStatus) *httptest.Server {
	t.Helper()

	runtime := &streamRuntime{}
	runtime.streaming = status.Streaming
	runtime.pending = status.Pending

	s := &loginService{
		cfg:    config.StreamConfig{CookieFile: filepath.Join(t.TempDir(), "cookie.txt")},
		stream: runtime,
	}

	return serveLogin(t, s)
}

func getBody(t *testing.T, url string) string {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应: %v", err)
	}

	return string(body)
}

// 60024：页面要给出可扫的二维码，PNG 端点要真出图。
func TestVerifyPageForQRCode(t *testing.T) {
	server := newVerifyLogin(t, streamStatus{Pending: &pendingVerify{
		Code: 60024,
		QR:   "https://example.com/verify?token=abc",
		At:   time.Now(),
	}})

	body := getBody(t, server.URL+loginVerifyPattern)
	if !strings.Contains(body, "扫码") || !strings.Contains(body, loginVerifyQRPattern) {
		t.Fatalf("验证页没有给出扫码入口:\n%s", body)
	}
	if !strings.Contains(body, "http-equiv=\"refresh\"") {
		t.Fatal("未推流时页面应当自动刷新，扫完才能自己接上")
	}

	resp, err := http.Get(server.URL + loginVerifyQRPattern)
	if err != nil {
		t.Fatalf("取验证二维码: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("验证二维码状态码 = %d", resp.StatusCode)
	}
	head := make([]byte, 8)
	if _, err := resp.Body.Read(head); err != nil {
		t.Fatalf("读验证二维码: %v", err)
	}
	if string(head[1:4]) != "PNG" {
		t.Fatalf("验证二维码不是 PNG: % x", head)
	}
}

// 60043：页面要给实名/人脸入口（链接来自登录态里的 mid）。
func TestVerifyPageForFaceAuth(t *testing.T) {
	server := newVerifyLogin(t, streamStatus{Pending: &pendingVerify{
		Code:     60043,
		FaceAuth: "https://www.bilibili.com/blackboard/live/face-auth-middle.html?source_event=400&mid=4987654",
		At:       time.Now(),
	}})

	body := getBody(t, server.URL+loginVerifyPattern)
	if !strings.Contains(body, "实名") || !strings.Contains(body, "mid=4987654") {
		t.Fatalf("验证页没有给出认证入口:\n%s", body)
	}
}

// 模板必须替我们转义：B 站 的文案是不可信输入（回归断言）。
func TestVerifyPageEscapesRemoteMessage(t *testing.T) {
	server := newVerifyLogin(t, streamStatus{Pending: &pendingVerify{
		Code:    60045,
		Message: `<script>alert(1)</script>` + "未满足开播条件",
		At:      time.Now(),
	}})

	body := getBody(t, server.URL+loginVerifyPattern)
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("B 站 返回的文案没有被转义:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("应当转义后原样展示:\n%s", body)
	}
	if !strings.Contains(body, "60045") {
		t.Fatalf("应当展示业务码便于定位:\n%s", body)
	}
}

// 已在推流：页面说清楚，并且不再自刷。
func TestVerifyPageWhileStreaming(t *testing.T) {
	server := newVerifyLogin(t, streamStatus{Streaming: true})

	body := getBody(t, server.URL+loginVerifyPattern)
	if !strings.Contains(body, "推流已在进行中") {
		t.Fatalf("推流中应当明确告知:\n%s", body)
	}
	if strings.Contains(body, "http-equiv=\"refresh\"") {
		t.Fatal("推流中不需要自动刷新")
	}
}

// 没有待扫码的验证时，验证二维码端点应当 404，而不是给出空图。
func TestVerifyQRCodeMissing(t *testing.T) {
	server := newVerifyLogin(t, streamStatus{})

	resp, err := http.Get(server.URL + loginVerifyQRPattern)
	if err != nil {
		t.Fatalf("取验证二维码: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", resp.StatusCode)
	}
}

// 账号准入（60045）这类「手机上也做不了什么」的拒绝必须照实显示。
//
// 真跑时踩到过：页面只记「需要用户操作」的码，60045 被当成没有 pending，
// 用户打开验证页只看到「还没有开播记录」——比日志还不如。
func TestVerifyPageShowsAccountDenial(t *testing.T) {
	server := newVerifyLogin(t, streamStatus{Pending: &pendingVerify{
		Code:    60045,
		Message: "非常抱歉，您账号在当前IP下未满足开播条件（注册时间>30天且粉丝数>500）",
		At:      time.Now(),
	}})

	body := getBody(t, server.URL+loginVerifyPattern)
	if !strings.Contains(body, "60045") || !strings.Contains(body, "未满足开播条件") {
		t.Fatalf("账号准入被拒应当在页面上说明:\n%s", body)
	}
}

// 网络这类没有业务码的失败也要显示，否则同样是「页面什么都不说」。
func TestVerifyPageShowsPlainFailure(t *testing.T) {
	server := newVerifyLogin(t, streamStatus{Pending: &pendingVerify{
		Message: "stream: 请求 B 站接口: dial tcp: i/o timeout",
		At:      time.Now(),
	}})

	body := getBody(t, server.URL+loginVerifyPattern)
	if !strings.Contains(body, "i/o timeout") {
		t.Fatalf("没有业务码的失败也应当显示:\n%s", body)
	}
}
