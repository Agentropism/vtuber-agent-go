package bilibili

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 开放平台 HTTP 接口路径。
const (
	pathStart     = "/v2/app/start"
	pathHeartbeat = "/v2/app/heartbeat"
	pathEnd       = "/v2/app/end"
)

// maxResponseSize 是 HTTP 响应体上限，防止异常响应撑爆内存。
const maxResponseSize = 1 << 20

// apiResponse 是开放平台 HTTP 接口的统一包装层。
type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// startRequest 是 /v2/app/start 的请求体。
//
// 字段顺序即序列化顺序：content-md5 覆盖的正是这些字节，顺序写错签名就全错。
type startRequest struct {
	Code  string `json:"code"`
	AppID int64  `json:"app_id"`
}

// endRequest 是 /v2/app/end 的请求体。
type endRequest struct {
	GameID string `json:"game_id"`
	AppID  int64  `json:"app_id"`
}

// heartbeatRequest 是 /v2/app/heartbeat 的请求体。
type heartbeatRequest struct {
	GameID string `json:"game_id"`
}

// startData 是 start 接口 data 字段里本包用到的部分。
type startData struct {
	GameInfo struct {
		GameID string `json:"game_id"`
	} `json:"game_info"`
	WebsocketInfo struct {
		AuthBody string   `json:"auth_body"`
		WssLink  []string `json:"wss_link"`
	} `json:"websocket_info"`
}

// call 调用一个开放平台接口，返回 data 字段的原文。
func (c *Client) call(ctx context.Context, path string, body any) (json.RawMessage, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Host+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("构造请求: %w", err)
	}
	for key, value := range c.signHeaders(payload) {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 响应: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s 返回 %d: %s", path, resp.StatusCode, clip(string(raw)))
	}

	var envelope apiResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("解析 %s 响应: %w（原文 %s）", path, err, clip(string(raw)))
	}
	// 原实现从不校验顶层 code，等到解析 data 失败才报错，真正的原因就丢了。
	if envelope.Code != 0 {
		return nil, fmt.Errorf("%s 返回 code=%d message=%s", path, envelope.Code, envelope.Message)
	}

	return envelope.Data, nil
}

// signHeaders 生成开放平台要求的签名头。
//
// 签名串是六行 "键:值"，按字典序排列、用 \n 连接、无尾随换行、无任何缩进——
// 原项目历史上就因为把签名串写成带缩进的多行字面量导致签名全错（提交 2ed5e73），
// 所以这里用显式的字符串拼接，绝不依赖源码缩进。
func (c *Client) signHeaders(payload []byte) map[string]string {
	sum := md5.Sum(payload)
	contentMD5 := hex.EncodeToString(sum[:])
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := newNonce()

	return map[string]string{
		"x-bili-accesskeyid":       c.cfg.AccessKey,
		"x-bili-content-md5":       contentMD5,
		"x-bili-signature-method":  "HMAC-SHA256",
		"x-bili-signature-nonce":   nonce,
		"x-bili-signature-version": "1.0",
		"x-bili-timestamp":         timestamp,
		"Authorization":            sign(c.cfg.AccessKeySecret, signSource(c.cfg.AccessKey, contentMD5, nonce, timestamp)),
	}
}

// signSource 拼接待签名的六行文本。
//
// 参数显式传入而不是就地取时间与随机数，是为了能用固定向量把「六行、LF 分隔、
// 无缩进、无尾随换行」这个布局钉死在测试里。
func signSource(accessKey, contentMD5, nonce, timestamp string) string {
	return strings.Join([]string{
		"x-bili-accesskeyid:" + accessKey,
		"x-bili-content-md5:" + contentMD5,
		"x-bili-signature-method:HMAC-SHA256",
		"x-bili-signature-nonce:" + nonce,
		"x-bili-signature-version:1.0",
		"x-bili-timestamp:" + timestamp,
	}, "\n")
}

// sign 用密钥对签名串做 HMAC-SHA256，返回小写 hex。
func sign(secret, source string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(source))

	return hex.EncodeToString(mac.Sum(nil))
}

// newNonce 生成小写带连字符的 UUID v4，不引入第三方依赖。
func newNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 随机源不可用属于环境异常：退化成时间戳，至少保证每次请求都不同。
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // 版本 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 变体

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// startApp 开始一次直播互动会话，拿到长连接地址与鉴权串。
func (c *Client) startApp(ctx context.Context) (startData, error) {
	var data startData

	raw, err := c.call(ctx, pathStart, startRequest{Code: c.cfg.IDCode, AppID: c.cfg.AppID})
	if err != nil {
		return data, err
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return data, fmt.Errorf("解析 start 响应: %w", err)
	}
	if len(data.WebsocketInfo.WssLink) == 0 {
		return data, errors.New("start 响应里没有可用的长连接地址")
	}
	if data.WebsocketInfo.AuthBody == "" {
		return data, errors.New("start 响应里没有鉴权串")
	}

	return data, nil
}

// heartbeat 上报应用心跳，维持互动会话。
func (c *Client) heartbeat(ctx context.Context, gameID string) error {
	_, err := c.call(ctx, pathHeartbeat, heartbeatRequest{GameID: gameID})

	return err
}

// endApp 结束互动会话。失败只记日志：进程正在退出，不值得再报错。
func (c *Client) endApp(gameID string) {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.CallTimeout)
	defer cancel()

	if _, err := c.call(ctx, pathEnd, endRequest{GameID: gameID, AppID: c.cfg.AppID}); err != nil {
		log.Sugar().Warnf("结束 B 站互动会话失败: %v", err)
		return
	}
	log.Sugar().Infof("已结束 B 站互动会话: game_id=%s", gameID)
}

// clip 截断过长的响应原文，避免刷屏。
func clip(text string) string {
	const limit = 200
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit]) + "…"
}
