package bilibili

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakePlatform 是一个假的 B 站开放平台：HTTP 三个接口 + 一个长连接端点。
type fakePlatform struct {
	mu            sync.Mutex
	startCalls    int
	heartbeatCall int
	endCalls      int
	authBodies    []string
	wsHeartbeats  int
}

func (f *fakePlatform) snapshot() (start, heartbeat, end, wsHeart int, auth []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCalls, f.heartbeatCall, f.endCalls, f.wsHeartbeats, append([]string(nil), f.authBodies...)
}

func (f *fakePlatform) writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]any{"code": 0, "message": "ok", "data": data})
	_, _ = w.Write(body)
}

// newFakePlatform 起一个假平台，events 是它在鉴权后依次推送的事件信封。
func newFakePlatform(t *testing.T, events [][]byte) (*fakePlatform, string, *httptest.Server) {
	t.Helper()

	fake := &fakePlatform{}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	mux.HandleFunc(pathStart, func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.startCalls++
		fake.mu.Unlock()

		fake.writeJSON(w, map[string]any{
			"game_info": map[string]any{"game_id": "game-1"},
			"websocket_info": map[string]any{
				"auth_body": `{"roomid":1906088607,"protover":2,"group":"open"}`,
				"wss_link":  []string{wsURL},
			},
		})
	})
	mux.HandleFunc(pathHeartbeat, func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.heartbeatCall++
		fake.mu.Unlock()
		fake.writeJSON(w, map[string]any{})
	})
	mux.HandleFunc(pathEnd, func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.endCalls++
		fake.mu.Unlock()
		fake.writeJSON(w, map[string]any{})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		defer conn.CloseNow()

		ctx := r.Context()

		// 第一帧必须是鉴权帧
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		packets, err := parsePackets(data)
		if err != nil || len(packets) != 1 {
			return
		}
		fake.mu.Lock()
		fake.authBodies = append(fake.authBodies, string(packets[0].body))
		fake.mu.Unlock()

		if err := conn.Write(ctx, websocket.MessageBinary,
			encodePacket(packet{operation: opAuthReply, body: []byte(`{"code":0}`)})); err != nil {
			return
		}

		// 鉴权成功后推送事件
		for _, event := range events {
			if err := conn.Write(ctx, websocket.MessageBinary, event); err != nil {
				return
			}
		}

		// 之后只处理心跳，读到断开为止
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			packets, err := parsePackets(data)
			if err != nil {
				continue
			}
			for _, p := range packets {
				if p.operation == opHeartbeat {
					fake.mu.Lock()
					fake.wsHeartbeats++
					fake.mu.Unlock()
				}
			}
		}
	})

	return fake, server.URL, server
}

// jsonEvent 造一个未压缩的事件帧。
func jsonEvent(t *testing.T, cmd, data string) []byte {
	t.Helper()
	return encodePacket(packet{
		version:   verJSON,
		operation: opMessage,
		body:      []byte(`{"cmd":"` + cmd + `","data":` + data + `}`),
	})
}

// zlibEvent 把若干事件帧压成一个 version=2 的帧。
func zlibEvent(t *testing.T, inner ...[]byte) []byte {
	t.Helper()

	var plain bytes.Buffer
	for _, frame := range inner {
		plain.Write(frame)
	}

	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(plain.Bytes()); err != nil {
		t.Fatalf("压缩: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭压缩流: %v", err)
	}

	return encodePacket(packet{version: verZlib, operation: opMessage, body: compressed.Bytes()})
}

// collector 收集处理器收到的事件。
type collector struct {
	mu       sync.Mutex
	payloads []string
	ch       chan string
}

func newCollector() *collector { return &collector{ch: make(chan string, 16)} }

func (c *collector) handle(payload []byte) {
	c.mu.Lock()
	c.payloads = append(c.payloads, string(payload))
	c.mu.Unlock()

	select {
	case c.ch <- string(payload):
	default:
	}
}

// waitFor 等一个包含子串的事件。
func (c *collector) waitFor(t *testing.T, substr string) string {
	t.Helper()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case got := <-c.ch:
			if strings.Contains(got, substr) {
				return got
			}
		case <-deadline:
			t.Fatalf("等待事件 %q 超时，已收到: %v", substr, c.snapshot())
		}
	}
}

func (c *collector) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.payloads...)
}

// 端到端：start → 选路 → 鉴权 → 收事件（含压缩帧）→ 心跳。
func TestClientEndToEnd(t *testing.T) {
	events := [][]byte{
		jsonEvent(t, "LIVE_OPEN_PLATFORM_DM", `{"msg":"普通帧","msg_id":"m1"}`),
		zlibEvent(t,
			jsonEvent(t, "LIVE_OPEN_PLATFORM_DM", `{"msg":"压缩帧一","msg_id":"m2"}`),
			jsonEvent(t, "LIVE_OPEN_PLATFORM_SEND_GIFT", `{"gift_name":"辣条","msg_id":"m3"}`),
		),
	}

	fake, host, server := newFakePlatform(t, events)
	defer server.Close()

	collected := newCollector()
	client, err := New(Config{
		Host:              host,
		AccessKey:         "test-key",
		AccessKeySecret:   "test-secret",
		IDCode:            "test-code",
		AppID:             123,
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectMin:      50 * time.Millisecond,
	}, collected.handle)
	if err != nil {
		t.Fatalf("构造客户端: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = client.Run(ctx) }()

	// 鉴权成功会补一条合成状态事件
	collected.waitFor(t, `"cmd":"status"`)
	// 未压缩帧
	collected.waitFor(t, "普通帧")
	// 压缩帧里的两个事件都必须被派发——这正是原 Rust 实现丢事件的地方
	collected.waitFor(t, "压缩帧一")
	collected.waitFor(t, "辣条")

	// 等两类心跳各来一次（HTTP 应用心跳与长连接心跳是两个独立协程，先后不定）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, heartbeat, _, wsHeart, _ := fake.snapshot()
		if heartbeat > 0 && wsHeart > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	start, heartbeat, _, wsHeart, authBodies := fake.snapshot()
	if start == 0 {
		t.Fatal("没有调用 start 接口")
	}
	if heartbeat == 0 {
		t.Fatal("没有调用应用心跳接口")
	}
	if wsHeart == 0 {
		t.Fatal("没有发送长连接心跳帧")
	}
	if len(authBodies) == 0 || authBodies[0] != `{"roomid":1906088607,"protover":2,"group":"open"}` {
		t.Fatalf("鉴权帧正文应是服务端下发的 auth_body 原文，实际 %v", authBodies)
	}

	// 取消后应当结束互动会话
	cancel()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, _, end, _, _ := fake.snapshot()
		if end > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("退出时没有调用 end 接口结束互动会话")
}

// 长连接断开后必须重连——原 Rust 实现的重连分支实际不可达。
func TestClientReconnects(t *testing.T) {
	fake, host, server := newFakePlatform(t, [][]byte{
		jsonEvent(t, "LIVE_OPEN_PLATFORM_DM", `{"msg":"第一段连接","msg_id":"m1"}`),
	})
	defer server.Close()

	collected := newCollector()
	client, err := New(Config{
		Host:              host,
		AccessKey:         "test-key",
		AccessKeySecret:   "test-secret",
		IDCode:            "test-code",
		AppID:             123,
		HeartbeatInterval: time.Hour, // 心跳不参与本用例
		ReadTimeout:       300 * time.Millisecond,
		ReconnectMin:      50 * time.Millisecond,
		ReconnectMax:      100 * time.Millisecond,
	}, collected.handle)
	if err != nil {
		t.Fatalf("构造客户端: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = client.Run(ctx) }()

	collected.waitFor(t, "第一段连接")

	// 服务端在推完事件后不再发任何东西，读超时会把这一段连接判死并触发重连
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		start, _, _, _, _ := fake.snapshot()
		if start >= 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	start, _, _, _, _ := fake.snapshot()
	t.Fatalf("读超时后没有重连，start 调用次数 = %d", start)
}

// 鉴权被拒绝时不能装作连上了：必须报错并重试。
func TestClientAuthRejected(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	starts := 0
	mux.HandleFunc(pathStart, func(w http.ResponseWriter, r *http.Request) {
		starts++
		fake := &fakePlatform{}
		fake.writeJSON(w, map[string]any{
			"game_info": map[string]any{"game_id": "game-1"},
			"websocket_info": map[string]any{
				"auth_body": `{"roomid":1}`,
				"wss_link":  []string{wsURL},
			},
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		ctx := r.Context()
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
		_ = conn.Write(ctx, websocket.MessageBinary,
			encodePacket(packet{operation: opAuthReply, body: []byte(`{"code":10001}`)}))
		// 鉴权失败后保持连接：客户端必须自己拆掉并重连，而不是空转
		<-ctx.Done()
	})

	client, err := New(Config{
		Host:            server.URL,
		AccessKey:       "test-key",
		AccessKeySecret: "test-secret",
		IDCode:          "test-code",
		AppID:           123,
		ReconnectMin:    50 * time.Millisecond,
		ReconnectMax:    50 * time.Millisecond,
	}, func([]byte) {})
	if err != nil {
		t.Fatalf("构造客户端: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = client.Run(ctx)

	if starts < 2 {
		t.Fatalf("鉴权失败后应重试，start 调用次数 = %d", starts)
	}
}
