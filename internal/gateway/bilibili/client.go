package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
)

// 默认参数。
const (
	defaultHost              = "https://live-open.biliapi.com"
	defaultHeartbeatInterval = 20 * time.Second
	defaultReadTimeout       = 90 * time.Second
	defaultCallTimeout       = 10 * time.Second
	defaultDialTimeout       = 5 * time.Second
	defaultReconnectMin      = time.Second
	defaultReconnectMax      = 30 * time.Second
	// wsConnectTimeout 是建立 WebSocket 连接的超时。
	wsConnectTimeout = 15 * time.Second
	// authTimeout 是等待鉴权回复的超时。
	authTimeout = 15 * time.Second
	// maxHeartbeatFailures 是应用心跳允许的连续失败次数，超过就整体重连。
	maxHeartbeatFailures = 5
	// maxWSMessageSize 是单条 WebSocket 消息的上限；一个消息里可能拼接多个包。
	maxWSMessageSize = 1 << 20
	// linkProbeTimeout 是选路时单条地址的拨号超时。
	linkProbeTimeout = 3 * time.Second
	// healthySessionDuration 是「这次连接算活得够久」的判据，用于重置退避。
	healthySessionDuration = time.Minute
)

// Config 是 B 站开放平台接入配置。
type Config struct {
	Host              string        // 开放平台 HTTP 地址，默认 https://live-open.biliapi.com
	AccessKey         string        // 开放平台 access key id
	AccessKeySecret   string        // 签名密钥
	IDCode            string        // 主播身份码
	AppID             int64         // 应用 ID
	HeartbeatInterval time.Duration // 心跳周期，默认 20s
	ReadTimeout       time.Duration // 读超时，默认 90s
	CallTimeout       time.Duration // 单次 HTTP 调用超时，默认 10s
	DialTimeout       time.Duration // 选路拨号超时，默认 5s（单条地址上限 3s）
	ReconnectMin      time.Duration // 重连退避下限，默认 1s
	ReconnectMax      time.Duration // 重连退避上限，默认 30s
}

// withDefaults 补齐未设置的字段。
func (c Config) withDefaults() Config {
	if c.Host == "" {
		c.Host = defaultHost
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = defaultHeartbeatInterval
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = defaultReadTimeout
	}
	if c.CallTimeout <= 0 {
		c.CallTimeout = defaultCallTimeout
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = defaultDialTimeout
	}
	if c.ReconnectMin <= 0 {
		c.ReconnectMin = defaultReconnectMin
	}
	if c.ReconnectMax <= 0 {
		c.ReconnectMax = defaultReconnectMax
	}
	if c.ReconnectMax < c.ReconnectMin {
		c.ReconnectMax = c.ReconnectMin
	}

	return c
}

// Handler 处理一条事件信封，形如 {"cmd":"LIVE_OPEN_PLATFORM_DM","data":{...}}。
//
// 处理是同步的：调用方在读到事件后立刻处理完才继续读下一帧。这与 WebSocket
// 接入路径的行为一致（server 也是在读循环里同步分发）。
type Handler func(payload []byte)

// Client 是 B 站开放平台长连接客户端。
type Client struct {
	cfg     Config
	http    *http.Client
	handler Handler
}

// New 构造客户端。
func New(cfg Config, handler Handler) (*Client, error) {
	if handler == nil {
		return nil, errors.New("bilibili: handler 不能为空")
	}
	cfg = cfg.withDefaults()
	if cfg.AccessKey == "" || cfg.AccessKeySecret == "" || cfg.IDCode == "" || cfg.AppID == 0 {
		return nil, errors.New("bilibili: access_key / access_key_secret / id_code / app_id 都必须配置")
	}

	return &Client{
		cfg:     cfg,
		http:    &http.Client{Timeout: cfg.CallTimeout},
		handler: handler,
	}, nil
}

// Run 启动客户端并阻塞到 ctx 取消；内部自带重连，不会因为网络抖动退出。
func (c *Client) Run(ctx context.Context) error {
	backoff := c.cfg.ReconnectMin

	for {
		started := time.Now()
		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 上一次连接活得够久说明只是偶发中断，退避从头算；反复失败才拉长间隔。
		if time.Since(started) > healthySessionDuration {
			backoff = c.cfg.ReconnectMin
		}

		log.Sugar().Warnf("B 站长连接中断: %v；%s 后重连", err, backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > c.cfg.ReconnectMax {
			backoff = c.cfg.ReconnectMax
		}
	}
}

// runOnce 完成一次「开会话 → 连长连接 → 鉴权 → 服务」的完整过程。
//
// 心跳、读循环任一出错都会结束本段会话并返回错误，由 Run 决定何时重连。
func (c *Client) runOnce(ctx context.Context) error {
	data, err := c.startApp(ctx)
	if err != nil {
		return err
	}
	log.Sugar().Infof("B 站互动会话已开始: game_id=%s", data.GameInfo.GameID)
	defer c.endApp(data.GameInfo.GameID)

	link, err := c.pickLink(ctx, data.WebsocketInfo.WssLink)
	if err != nil {
		return err
	}

	conn, err := c.dial(ctx, link)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := c.authenticate(sessionCtx, conn, data.WebsocketInfo.AuthBody); err != nil {
		return err
	}
	log.Sugar().Infof("B 站长连接已就绪: %s", link)

	// 与原实现一致：鉴权成功后向下游补一条合成状态事件。
	c.handler([]byte(`{"cmd":"status","data":true}`))

	appHeart := make(chan error, 1)
	wsHeart := make(chan error, 1)
	readDone := make(chan error, 1)
	go func() { appHeart <- c.runAppHeartbeat(sessionCtx, data.GameInfo.GameID) }()
	go func() { wsHeart <- c.runWSHeartbeat(sessionCtx, conn) }()
	go func() { readDone <- c.readLoop(sessionCtx, conn) }()

	select {
	case <-sessionCtx.Done():
		return sessionCtx.Err()
	case err := <-appHeart:
		return err
	case err := <-wsHeart:
		return err
	case err := <-readDone:
		return err
	}
}

// pickLink 从服务端下发的地址里选一个可用的。
//
// 原实现用 ICMP ping 测 RTT，且任一条地址不通就整体启动失败（还需要 ping 权限）。
// 这里改成逐个 TCP 拨号测速：不可达的直接跳过，全都不可达才报错。
func (c *Client) pickLink(ctx context.Context, links []string) (string, error) {
	var (
		best     string
		bestCost = time.Duration(1<<62 - 1)
	)
	for _, link := range links {
		cost, err := probeLink(ctx, link)
		if err != nil {
			log.Sugar().Debugf("跳过不可达的长连接地址 %s: %v", link, err)
			continue
		}
		if cost < bestCost {
			best, bestCost = link, cost
		}
	}
	if best == "" {
		return "", fmt.Errorf("没有可用的长连接地址（服务端下发 %d 条）", len(links))
	}
	log.Sugar().Debugf("选用长连接地址 %s（握手耗时 %s）", best, bestCost)

	return best, nil
}

// probeLink 用一次 TCP 拨号估算到该地址的耗时。
func probeLink(ctx context.Context, link string) (time.Duration, error) {
	parsed, err := url.Parse(link)
	if err != nil {
		return 0, err
	}
	host := parsed.Host
	if parsed.Port() == "" {
		host = net.JoinHostPort(parsed.Hostname(), "443")
	}

	dialCtx, cancel := context.WithTimeout(ctx, linkProbeTimeout)
	defer cancel()

	start := time.Now()
	var dialer net.Dialer
	conn, err := dialer.DialContext(dialCtx, "tcp", host)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()

	return time.Since(start), nil
}

// dial 建立 WebSocket 连接。
func (c *Client) dial(ctx context.Context, link string) (*websocket.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, wsConnectTimeout)
	defer cancel()

	conn, resp, err := websocket.Dial(dialCtx, link, nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("连接 %s: %w", link, err)
	}
	conn.SetReadLimit(maxWSMessageSize)

	return conn, nil
}

// authenticate 发送鉴权帧并等待鉴权回复。
//
// 鉴权帧：version=0、operation=7、sequence=0，body 是服务端下发的 auth_body 原文
// （它本身就是一段 JSON 字符串，不能再序列化一次）。
func (c *Client) authenticate(ctx context.Context, conn *websocket.Conn, authBody string) error {
	frame := encodePacket(packet{
		version:   verPlain,
		operation: opAuth,
		body:      []byte(authBody),
	})

	writeCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	err := conn.Write(writeCtx, websocket.MessageBinary, frame)
	cancel()
	if err != nil {
		return fmt.Errorf("发送鉴权帧: %w", err)
	}

	deadline := time.Now().Add(authTimeout)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(ctx, time.Until(deadline))
		_, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("等待鉴权回复: %w", err)
		}

		packets, err := parsePackets(data)
		if err != nil {
			return fmt.Errorf("解析鉴权回复: %w", err)
		}
		for _, p := range packets {
			if p.operation != opAuthReply {
				continue
			}

			var resp struct {
				Code *int `json:"code"`
			}
			if err := json.Unmarshal(p.body, &resp); err != nil {
				return fmt.Errorf("解析鉴权回复体: %w", err)
			}
			// 原实现遇到鉴权失败只记日志、不拆连接，客户端会以未鉴权状态永久空转。
			if resp.Code == nil {
				return errors.New("鉴权回复里没有 code 字段")
			}
			if *resp.Code != 0 {
				return fmt.Errorf("鉴权被拒绝: code=%d", *resp.Code)
			}

			return nil
		}
	}

	return errors.New("等待鉴权回复超时")
}

// runWSHeartbeat 按周期发送长连接心跳帧（operation=2，无 body，序号自增）。
func (c *Client) runWSHeartbeat(ctx context.Context, conn *websocket.Conn) error {
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()

	var sequence int32
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			sequence++
			frame := encodePacket(packet{
				version:   verPlain,
				operation: opHeartbeat,
				sequence:  sequence,
			})

			writeCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
			err := conn.Write(writeCtx, websocket.MessageBinary, frame)
			cancel()
			if err != nil {
				return fmt.Errorf("发送长连接心跳: %w", err)
			}
		}
	}
}

// runAppHeartbeat 按周期上报应用心跳，连续失败到上限就整体重连。
func (c *Client) runAppHeartbeat(ctx context.Context, gameID string) error {
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := c.heartbeat(ctx, gameID); err != nil {
				failures++
				log.Sugar().Warnf("B 站应用心跳失败（连续 %d/%d 次）: %v", failures, maxHeartbeatFailures, err)
				if failures >= maxHeartbeatFailures {
					return fmt.Errorf("应用心跳连续失败 %d 次: %w", failures, err)
				}
				continue
			}
			failures = 0
		}
	}
}

// readLoop 持续读取长连接消息并派发事件。
//
// 每次读取都带超时：服务端每 20 秒至少有一次心跳回复，长时间收不到任何字节
// 说明连接已经半开，交给重连处理（原实现没有读超时，断连后只能靠写心跳才发现）。
func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		readCtx, cancel := context.WithTimeout(ctx, c.cfg.ReadTimeout)
		msgType, data, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("读取长连接消息: %w", err)
		}
		if msgType != websocket.MessageBinary {
			log.Sugar().Debugf("忽略非二进制帧: %v", msgType)
			continue
		}

		packets, err := parsePackets(data)
		if err != nil {
			log.Sugar().Warnf("解析 B 站消息帧失败: %v", err)
			continue
		}
		for _, p := range packets {
			c.dispatch(p)
		}
	}
}

// dispatch 按 operation 处理一个协议包。
func (c *Client) dispatch(p packet) {
	switch p.operation {
	case opMessage:
		c.handler(p.body)
	case opHeartbeatReply:
		log.Sugar().Debugf("B 站心跳回复: sequence=%d", p.sequence)
	case opAuthReply:
		log.Sugar().Debugf("收到重复的鉴权回复，忽略")
	default:
		log.Sugar().Debugf("忽略未知 operation=%d", p.operation)
	}
}
