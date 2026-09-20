package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// 本文件实现 B 站扫码登录：申请二维码 → 轮询状态 → 从 Set-Cookie 里取登录态。
//
// 接口与流程照 bili-live-hime（https://github.com/Rsplwe/bili-live-hime）：
// passport 的 generate + poll。关键区别是**登录成功的凭据只在响应头里**，
// 响应体只给一个状态码，所以轮询必须看 Set-Cookie。
//
// 拿到的是网页登录态（SESSDATA / bili_jct），随后交给开播接口（见同包 bilibili.go）。
const (
	loginBaseURL      = "https://passport.bilibili.com"
	loginGeneratePath = "/x/passport-login/web/qrcode/generate"
	loginPollPath     = "/x/passport-login/web/qrcode/poll"

	// 轮询响应体里的业务状态码。
	loginCodeSuccess = 0
	loginCodeExpired = 86038
	loginCodeScanned = 86090
	loginCodeWaiting = 86101
)

// cookieForLogin 是登录成功必须拿到的两项；缺了就按登录失败处理。
//
// 其余的 cookie（buvid3 / buvid4 / b_nut 等）同样要保留并回发，见 cookieValue。
var cookieForLogin = []string{"SESSDATA", "bili_jct"}

// LoginState 是二维码的当前状态，直接透给前端。
type LoginState string

const (
	// LoginWaiting 还没人扫。
	LoginWaiting LoginState = "waiting"
	// LoginScanned 已扫码，等手机确认。
	LoginScanned LoginState = "scanned"
	// LoginConfirmed 已确认，cookie 可用。
	LoginConfirmed LoginState = "confirmed"
	// LoginExpired 二维码过期，要重新申请。
	LoginExpired LoginState = "expired"
)

// LoginConfig 是登录接口的可覆盖项（BaseURL / Client 供测试注入）。
type LoginConfig struct {
	BaseURL string
	Client  *http.Client
}

// LoginQRCode 是一次登录会话。
type LoginQRCode struct {
	URL string // 二维码内容（B 站登录页地址）
	Key string // 轮询用的 key
}

// LoginResult 是一次轮询的结果。
type LoginResult struct {
	State   LoginState
	Cookie  string // 仅 confirmed 时非空
	Message string // B 站给的提示
}

// GenerateLoginQRCode 申请一张新二维码。
func GenerateLoginQRCode(ctx context.Context, cfg LoginConfig) (LoginQRCode, error) {
	base := loginBase(cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+loginGeneratePath, nil)
	if err != nil {
		return LoginQRCode{}, fmt.Errorf("stream: 构造请求: %w", err)
	}

	body, _, err := requestBili(ctx, base, cfg.Client, req)
	if err != nil {
		return LoginQRCode{}, err
	}

	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			URL       string `json:"url"`
			QRCodeKey string `json:"qrcode_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return LoginQRCode{}, fmt.Errorf("stream: 解析二维码响应: %w", err)
	}
	if payload.Code != 0 || payload.Data == nil || payload.Data.URL == "" || payload.Data.QRCodeKey == "" {
		return LoginQRCode{}, fmt.Errorf("stream: 申请二维码失败(%d): %s", payload.Code, payload.Message)
	}

	return LoginQRCode{URL: payload.Data.URL, Key: payload.Data.QRCodeKey}, nil
}

// PollLogin 轮询一次登录状态；确认后返回可用的 cookie。
func PollLogin(ctx context.Context, cfg LoginConfig, key string) (LoginResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return LoginResult{}, errors.New("stream: 轮询登录缺少二维码 key")
	}

	base := loginBase(cfg)
	path := loginPollPath + "?qrcode_key=" + url.QueryEscape(key)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return LoginResult{}, fmt.Errorf("stream: 构造请求: %w", err)
	}

	body, cookies, err := requestBili(ctx, base, cfg.Client, req)
	if err != nil {
		return LoginResult{}, err
	}

	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return LoginResult{}, fmt.Errorf("stream: 解析轮询响应: %w", err)
	}
	if payload.Code != 0 || payload.Data == nil {
		return LoginResult{}, fmt.Errorf("stream: 轮询登录失败(%d): %s", payload.Code, payload.Message)
	}

	switch payload.Data.Code {
	case loginCodeSuccess:
		// 带上 jar 里的 cookie：指纹 cookie（buvid3 等）通常在 generate 阶段就下发，
		// 只留 poll 响应里的那几个，请求看起来还是陌生设备。
		all := append(jarCookies(cfg.Client, base), cookies...)
		if missing := missingLoginCookies(all); len(missing) > 0 {
			// 必需项缺失：当成失败，不能揣着半个凭据往下走
			return LoginResult{}, fmt.Errorf("stream: 登录已确认但缺少 %s", strings.Join(missing, "、"))
		}

		return LoginResult{State: LoginConfirmed, Cookie: cookieValue(all), Message: "登录成功"}, nil
	case loginCodeScanned:
		return LoginResult{State: LoginScanned, Message: payload.Data.Message}, nil
	case loginCodeExpired:
		return LoginResult{State: LoginExpired, Message: payload.Data.Message}, nil
	case loginCodeWaiting:
		return LoginResult{State: LoginWaiting, Message: payload.Data.Message}, nil
	default:
		// 没见过的码不当成成功也不当成失败：交给前端继续轮询，过期由超时兜底
		return LoginResult{State: LoginWaiting, Message: payload.Data.Message}, nil
	}
}

// cookieValue 把 Set-Cookie 全量拼成 Cookie 串。
//
// **不挑不拣**：B 站 还会下发 buvid3 / buvid4 / b_nut 这类设备指纹 cookie，风控拿它们
// 判断「这个请求像不像已知的浏览器会话」。参考项目就是全量存、全量回发；我们先前只挑
// 4 个必要项，等于每次请求都以陌生设备身份出现（同一账号在参考工具里能开播，在这里
// 被 60045 开播准入拒掉——这是最可疑的差异）。
func cookieValue(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	seen := make(map[string]bool, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name == "" || cookie.Value == "" || seen[cookie.Name] {
			continue
		}
		seen[cookie.Name] = true
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}

	return strings.Join(parts, "; ")
}

// jarCookies 取 jar 里与登录相关的 cookie。
//
// 不能只按 base 查：Set-Cookie 没写 Path 时，jar 会按「请求路径的目录」给 cookie 定作用域
// （RFC 6265 的默认路径），而 generate 的路径是 /x/passport-login/web/qrcode/generate。
// 三个 URL 都查一遍再合并，指纹 cookie 才不会漏（这条是测试逼出来的）。
func jarCookies(client *http.Client, base string) []*http.Cookie {
	if client == nil || client.Jar == nil {
		return nil
	}

	var out []*http.Cookie
	seen := make(map[string]bool)
	for _, raw := range []string{base, base + loginGeneratePath, base + loginPollPath} {
		parsed, err := url.Parse(raw)
		if err != nil {
			continue
		}
		for _, cookie := range client.Jar.Cookies(parsed) {
			if seen[cookie.Name] {
				continue
			}
			seen[cookie.Name] = true
			out = append(out, cookie)
		}
	}

	return out
}

// missingLoginCookies 列出登录必需但没拿到的 cookie。
func missingLoginCookies(cookies []*http.Cookie) []string {
	have := make(map[string]bool, len(cookies))
	for _, cookie := range cookies {
		have[cookie.Name] = true
	}

	var missing []string
	for _, name := range cookieForLogin {
		if !have[name] {
			missing = append(missing, name)
		}
	}

	return missing
}

// SaveLoginCookie 把登录态落盘，供下次启动直接复用。
//
// 权限 0600：这是能开播、能发弹幕的凭据，同机其他用户读到就等于拿到账号。
func SaveLoginCookie(path, cookie string) error {
	if strings.TrimSpace(cookie) == "" {
		// pi-lens-ignore: go-bare-error — 这里返回的是刚新建的错误，不是漏处理的 err
		return errors.New("stream: 拒绝写入空 cookie")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("stream: 创建 cookie 目录: %w", err)
		}
	}

	return os.WriteFile(path, []byte(cookie), 0o600)
}

// LoadLoginCookie 读回登录态；文件不存在或读不动时返回空串（调用方按「未登录」处理）。
func LoadLoginCookie(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func loginBase(cfg LoginConfig) string {
	if cfg.BaseURL != "" {
		return cfg.BaseURL
	}

	return loginBaseURL
}
