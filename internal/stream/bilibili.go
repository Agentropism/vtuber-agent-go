package stream

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 本文件只做一件事：用主播自己的 cookie 调 B 站 web 接口开播，拿回推流地址与密钥。
//
// 接口与签名照 bili-live-hime 的实现（https://github.com/Rsplwe/bili-live-hime）：
// appkey/appsec 是官方 web 客户端内置的那一对，sign = md5(排序后的表单 + appsec)，
// 请求体是 x-www-form-urlencoded，Cookie 里必须有 SESSDATA 与 bili_jct。
//
// 注意：这套是 web 接口，不是官方开放平台，属于网页登录态玩法。风控会看 UA 与
// 请求头，所以这些头是照浏览器抄的，别随便改。
const (
	biliAppKey  = "aae92bc66f3edfab"
	biliAppSec  = "af125a0d5279fd576c1b4418a3e8276d"
	biliBaseURL = "https://api.live.bilibili.com"
	biliUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36 Edg/143.0.0.0"

	// liveCodeNeedVerify 是「需要扫码/人脸验证」；liveCodeNeedRealName 是未实名。
	liveCodeNeedVerify   = 60024
	liveCodeNeedRealName = 60043
)

// LiveConfig 是开播所需的最小信息。
type LiveConfig struct {
	// Cookie 是浏览器 Cookie，至少含 SESSDATA 与 bili_jct。凭据不进仓库。
	Cookie string
	// RoomID 是直播间号。
	RoomID int64
	// AreaID 是分区 id；留空表示沿用直播间当前分区。
	AreaID string
	// BaseURL 仅供测试覆盖，留空用官方地址。
	BaseURL string
	// Client 仅供测试覆盖，留空用默认客户端。
	Client *http.Client
}

// LiveInfo 是 startLive 返回的推流信息。
type LiveInfo struct {
	Addr string // 形如 rtmp://live-push.bilivideo.com/live-bvc/?streamname=...
	Key  string // 推流密钥
}

// Output 拼成 ffmpeg 直接可用的推流地址（地址与密钥合并）。
func (i LiveInfo) Output() string {
	if i.Key == "" {
		return i.Addr
	}
	if strings.Contains(i.Addr, "?") {
		return i.Addr + "&key=" + i.Key
	}

	return i.Addr + "?key=" + i.Key
}

// appSign 给表单加上 appkey 与 sign，返回已编码的请求体。
//
// 参考实现用的是 URLSearchParams：先按 key 排序再拼接，然后 md5(拼接串 + appsec)。
// url.Values.Encode() 同样是按 key 排序 + 百分号编码，两边结果一致。
func appSign(params map[string]string) string {
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	values.Set("appkey", biliAppKey)

	query := values.Encode()
	// md5 是 B 站接口的硬要求（sign=md5(表单+appsec)），不是这里的加密选择：
	// 换 sha256 会导致接口直接拒签。它是签名算法，不是口令存储或校验。
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-md5
	// pi-lens-ignore: go-weak-hash
	sum := md5.Sum([]byte(query + biliAppSec))

	return query + "&sign=" + hex.EncodeToString(sum[:])
}

// StartLive 开播并返回推流地址与密钥。
func StartLive(ctx context.Context, cfg LiveConfig) (LiveInfo, error) {
	if strings.TrimSpace(cfg.Cookie) == "" {
		return LiveInfo{}, errors.New("stream: 未配置 B 站 cookie，无法开播")
	}
	if cfg.RoomID == 0 {
		return LiveInfo{}, errors.New("stream: 未配置直播间号 room_id")
	}

	version, build, err := liveVersion(ctx, cfg)
	if err != nil {
		return LiveInfo{}, err
	}

	body := appSign(map[string]string{
		"room_id":       strconv.FormatInt(cfg.RoomID, 10),
		"platform":      "pc_link",
		"backup_stream": "0",
		"csrf":          csrf(cfg.Cookie),
		"csrf_token":    csrf(cfg.Cookie),
		"area_v2":       cfg.AreaID,
		"version":       version,
		"build":         build,
		"ts":            strconv.FormatInt(time.Now().UnixMilli(), 10),
	})

	resp, err := postForm(ctx, cfg, "/room/v1/Room/startLive", body)
	if err != nil {
		return LiveInfo{}, err
	}

	var payload struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    *startLiveData `json:"data"`
	}
	if err := json.Unmarshal(resp, &payload); err != nil {
		return LiveInfo{}, fmt.Errorf("stream: 解析开播响应: %w", err)
	}

	switch payload.Code {
	case 0:
	case liveCodeNeedVerify, liveCodeNeedRealName:
		// 这两类不是「配置写错了」，是要用户拿手机做一步，必须把入口带出去
		return LiveInfo{}, &StartLiveError{
			Code:     payload.Code,
			Message:  payload.Message,
			QR:       qrOf(payload.Data),
			FaceAuth: faceAuthURL(cfg.Cookie),
		}
	default:
		return LiveInfo{}, &StartLiveError{Code: payload.Code, Message: payload.Message}
	}

	if payload.Data == nil || payload.Data.RTMP.Addr == "" {
		return LiveInfo{}, errors.New("stream: 开播成功但响应里没有推流地址")
	}

	return LiveInfo{Addr: payload.Data.RTMP.Addr, Key: payload.Data.RTMP.Code}, nil
}

// startLiveData 是 startLive 的 data 段：成功时给 rtmp，要验证时给 qr。
type startLiveData struct {
	QR   string `json:"qr"`
	RTMP struct {
		Addr string `json:"addr"`
		Code string `json:"code"`
	} `json:"rtmp"`
}

// StartLiveError 是开播被拒的结构化错误。
//
// 带上业务码与 B 站 给的验证入口：60024（要扫码验证）与 60043（要实名/人脸）不是
// 「配置写错了」，而是要用户拿手机做一步。只丢一句日志等于把用户堵在死胡同里。
type StartLiveError struct {
	Code    int
	Message string
	// QR 是 60024 时给用户扫的地址。
	QR string
	// FaceAuth 是 60043 时的实名/人脸认证页（带登录态里的 mid）。
	FaceAuth string
}

func (e *StartLiveError) Error() string {
	switch e.Code {
	case liveCodeNeedVerify:
		return "stream: 开播需要扫码验证（用 B 站 App 扫 /login/verify 上的二维码）"
	case liveCodeNeedRealName:
		return "stream: 开播需要先完成实名/人脸认证: " + e.FaceAuth
	default:
		return fmt.Sprintf("stream: 开播失败(%d): %s", e.Code, e.Message)
	}
}

// NeedsUserAction 表示这次拒绝需要用户在手机上做一步，重试间隔该短一些。
func (e *StartLiveError) NeedsUserAction() bool {
	return e.Code == liveCodeNeedVerify || e.Code == liveCodeNeedRealName
}

// qrOf 取 60024 的验证地址；data 可能是 nil（别的码不保证带 data）。
func qrOf(data *startLiveData) string {
	if data == nil {
		return ""
	}

	return data.QR
}

// faceAuthURL 拼直播实名/人脸认证页；mid 取自登录态里的 DedeUserID。
//
// 地址与 bili-live-hime 用的同一个（source_event=400 是「开播需要认证」这个入口）。
func faceAuthURL(cookie string) string {
	const base = "https://www.bilibili.com/blackboard/live/face-auth-middle.html?source_event=400"
	if mid := cookieValueFor(cookie, "DedeUserID"); mid != "" {
		return base + "&mid=" + url.QueryEscape(mid)
	}

	return base
}

// StopLive 关播。进程退出时调用，避免直播间挂着「直播中」。
func StopLive(ctx context.Context, cfg LiveConfig) error {
	values := url.Values{}
	values.Set("room_id", strconv.FormatInt(cfg.RoomID, 10))
	values.Set("platform", "pc_link")
	values.Set("csrf", csrf(cfg.Cookie))
	values.Set("csrf_token", csrf(cfg.Cookie))

	if _, err := postForm(ctx, cfg, "/room/v1/Room/stopLive", values.Encode()); err != nil {
		return fmt.Errorf("stream: 关播失败: %w", err)
	}

	return nil
}

// liveVersion 取 web 客户端当前版本号，startLive 必须带对，否则会被拒。
func liveVersion(ctx context.Context, cfg LiveConfig) (version string, build string, err error) {
	params := map[string]string{
		"system_version": "2",
		"ts":             strconv.FormatInt(time.Now().UnixMilli(), 10),
	}

	resp, err := getJSON(ctx, cfg, "/xlive/app-blink/v1/liveVersionInfo/getHomePageLiveVersion?"+appSign(params))
	if err != nil {
		return "", "", fmt.Errorf("取开播版本: %w", err)
	}

	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			CurrVersion string `json:"curr_version"`
			Build       int    `json:"build"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &payload); err != nil {
		return "", "", fmt.Errorf("stream: 解析版本响应: %w", err)
	}
	if payload.Code != 0 || payload.Data == nil {
		return "", "", fmt.Errorf("stream: 取开播版本失败(%d): %s", payload.Code, payload.Message)
	}

	return payload.Data.CurrVersion, strconv.Itoa(payload.Data.Build), nil
}

// csrf 从 cookie 里取 bili_jct，B 站写接口都要它。
func csrf(cookie string) string {
	return cookieValueFor(cookie, "bili_jct")
}

// cookieValueFor 从 Cookie 串里取某一项的值。
func cookieValueFor(cookie, name string) string {
	for _, part := range strings.Split(cookie, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && key == name {
			return value
		}
	}

	return ""
}

func postForm(ctx context.Context, cfg LiveConfig, path, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL(cfg)+path, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("stream: 构造请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	resp, err := do(ctx, cfg, req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s: %w", path, err)
	}

	return resp, nil
}

func getJSON(ctx context.Context, cfg LiveConfig, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL(cfg)+path, nil)
	if err != nil {
		return nil, fmt.Errorf("stream: 构造请求: %w", err)
	}

	body, err := do(ctx, cfg, req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s: %w", path, err)
	}

	return body, nil
}

func do(ctx context.Context, cfg LiveConfig, req *http.Request) ([]byte, error) {
	if cfg.Cookie != "" {
		req.Header.Set("Cookie", cfg.Cookie)
	}

	body, _, err := requestBili(ctx, baseURL(cfg), cfg.Client, req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s: %w", req.URL.Path, err)
	}

	return body, nil
}

// requestBili 发一次 B 站 web 请求，返回响应体与 Set-Cookie。
//
// Set-Cookie 也要返回：扫码登录成功的凭据只在响应头里，不在响应体里
// （见 bilibili_login.go）。请求头照浏览器抄，风控看这些。
func requestBili(ctx context.Context, base string, client *http.Client, req *http.Request) ([]byte, []*http.Cookie, error) {
	req.Header.Set("User-Agent", biliUA)
	req.Header.Set("Origin", base)
	req.Header.Set("Referer", base+"/")
	req.Header.Set("Accept", "*/*")

	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("stream: 请求 B 站接口: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("stream: 读取响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("stream: B 站接口返回 %d: %s", resp.StatusCode, truncate(string(data)))
	}

	return data, resp.Cookies(), nil
}

func baseURL(cfg LiveConfig) string {
	if cfg.BaseURL != "" {
		return cfg.BaseURL
	}

	return biliBaseURL
}

func truncate(text string) string {
	if len(text) > 200 {
		return text[:200] + "…"
	}

	return text
}
