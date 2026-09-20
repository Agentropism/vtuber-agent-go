package stream

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateLoginQRCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != loginGeneratePath {
			t.Errorf("路径不对: %s", r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("User-Agent"), "Mozilla") {
			t.Errorf("UA 不像浏览器: %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://passport.example/h5?k=1","qrcode_key":"key-123"}}`))
	}))
	defer server.Close()

	qr, err := GenerateLoginQRCode(context.Background(), LoginConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("申请二维码: %v", err)
	}
	if qr.URL != "https://passport.example/h5?k=1" || qr.Key != "key-123" {
		t.Fatalf("二维码解析错: %+v", qr)
	}
}

func TestGenerateLoginQRCodeRejectsBadPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":-412,"message":"请求被拦截","data":null}`))
	}))
	defer server.Close()

	if _, err := GenerateLoginQRCode(context.Background(), LoginConfig{BaseURL: server.URL}); err == nil {
		t.Fatal("接口报错时应当返回错误")
	}
}

// 轮询的四个状态各自可辨，别把「已扫码待确认」当成「没扫」让用户干等。
func TestPollLoginStates(t *testing.T) {
	cases := []struct {
		name  string
		code  int
		want  LoginState
		extra string
	}{
		{"未扫码", loginCodeWaiting, LoginWaiting, ""},
		{"已扫码待确认", loginCodeScanned, LoginScanned, ""},
		{"已过期", loginCodeExpired, LoginExpired, ""},
		{"没见过的码按等待处理", 12345, LoginWaiting, ""},
		{"确认登录", loginCodeSuccess, LoginConfirmed, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.RawQuery, "qrcode_key=key-123") {
					t.Errorf("轮询没有带 qrcode_key: %s", r.URL.RawQuery)
				}
				// 登录成功时凭据只在响应头里
				if tc.code == loginCodeSuccess {
					http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess"})
					http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "jct"})
					http.SetCookie(w, &http.Cookie{Name: "DedeUserID", Value: "42"})
					// 设备指纹 cookie：风控靠它判断「像不像已知浏览器会话」，必须一起留下来
					http.SetCookie(w, &http.Cookie{Name: "buvid3", Value: "fp-a"})
				}
				_, _ = w.Write([]byte(`{"code":0,"data":{"code":` + itoa(tc.code) + `,"message":"msg"}}`))
			}))
			defer server.Close()

			result, err := PollLogin(context.Background(), LoginConfig{BaseURL: server.URL}, "key-123")
			if err != nil {
				t.Fatalf("轮询: %v", err)
			}
			if result.State != tc.want {
				t.Fatalf("状态 = %q, want %q", result.State, tc.want)
			}
			if tc.want == LoginConfirmed {
				if !strings.Contains(result.Cookie, "SESSDATA=sess") || !strings.Contains(result.Cookie, "bili_jct=jct") {
					t.Fatalf("cookie 不完整: %q", result.Cookie)
				}
				if !strings.Contains(result.Cookie, "buvid3=fp-a") {
					t.Fatalf("指纹 cookie 必须一起留下（风控看它）: %q", result.Cookie)
				}
			} else if result.Cookie != "" {
				t.Fatalf("非确认状态不该带 cookie: %q", result.Cookie)
			}
		})
	}
}

// 必需项缺失时按失败处理：半套凭据拿去开播只会得到莫名其妙的错误。
func TestPollLoginMissingEssentialCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess"})
		// 故意不给 bili_jct
		_, _ = w.Write([]byte(`{"code":0,"data":{"code":0,"message":"ok"}}`))
	}))
	defer server.Close()

	_, err := PollLogin(context.Background(), LoginConfig{BaseURL: server.URL}, "key-123")
	if err == nil || !strings.Contains(err.Error(), "bili_jct") {
		t.Fatalf("缺 bili_jct 应当报错并点名: %v", err)
	}
}

// 状态说成功、响应头却是空的：当成失败，不能揣着空凭据往下走。
func TestPollLoginConfirmedWithoutCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"code":0,"message":"ok"}}`))
	}))
	defer server.Close()

	if _, err := PollLogin(context.Background(), LoginConfig{BaseURL: server.URL}, "key-123"); err == nil {
		t.Fatal("confirmed 但没有 cookie 应当报错")
	}
}

func TestPollLoginRequiresKey(t *testing.T) {
	if _, err := PollLogin(context.Background(), LoginConfig{}, "  "); err == nil {
		t.Fatal("没有 key 应当报错")
	}
}

// 登录态落盘必须只有本人可读：这是能开播、能发弹幕的凭据。
func TestSaveAndLoadLoginCookie(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cookie.txt")

	if err := SaveLoginCookie(path, "SESSDATA=s; bili_jct=j"); err != nil {
		t.Fatalf("保存: %v", err)
	}
	if got := LoadLoginCookie(path); got != "SESSDATA=s; bili_jct=j" {
		t.Fatalf("读回 = %q", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("权限 = %o, want 600", perm)
	}

	if err := SaveLoginCookie(path, "   "); err == nil {
		t.Fatal("空 cookie 应当拒绝写入")
	}
	if got := LoadLoginCookie(filepath.Join(t.TempDir(), "missing.txt")); got != "" {
		t.Fatalf("文件不存在应返回空串，得到 %q", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	return sign + string(digits)
}

// 带 jar 的客户端必须把 generate 阶段下发的指纹 cookie 一起带进最终凭据。
//
// 这是「请求看起来像不像同一个浏览器会话」的关键：只留 poll 响应里的凭据，
// buvid3 这类设备指纹就丢了，请求会以陌生设备身份出现。
func TestLoginKeepsFingerprintCookieFromGenerate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/generate"):
			http.SetCookie(w, &http.Cookie{Name: "buvid3", Value: "fp-1"})
			_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://example.com/h5","qrcode_key":"k1"}}`))
		case strings.HasSuffix(r.URL.Path, "/poll"):
			// 这条响应只给凭据，指纹 cookie 是上一步给的
			http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess"})
			http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "jct"})
			_, _ = w.Write([]byte(`{"code":0,"data":{"code":0,"message":"ok"}}`))
		default:
			t.Errorf("意外的路径: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 jar: %v", err)
	}
	cfg := LoginConfig{BaseURL: server.URL, Client: &http.Client{Jar: jar}}

	if _, err := GenerateLoginQRCode(context.Background(), cfg); err != nil {
		t.Fatalf("申请二维码: %v", err)
	}
	result, err := PollLogin(context.Background(), cfg, "k1")
	if err != nil {
		t.Fatalf("轮询: %v", err)
	}

	for _, want := range []string{"SESSDATA=sess", "bili_jct=jct", "buvid3=fp-1"} {
		if !strings.Contains(result.Cookie, want) {
			t.Fatalf("最终凭据缺少 %q: %q", want, result.Cookie)
		}
	}
}
