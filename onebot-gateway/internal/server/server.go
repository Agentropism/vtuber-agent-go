package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"onebot-gateway/internal/action"
	"onebot-gateway/internal/config"
	"onebot-gateway/internal/event"
	"onebot-gateway/internal/upload"

	"github.com/coder/websocket"
	"go.uber.org/zap"
)

type contextKey string

const platformKey contextKey = "platform"

type clientConnection struct {
	conn      *websocket.Conn
	writeFunc func(context.Context, []byte) error
	mu        sync.Mutex
}

var clients = struct {
	sync.RWMutex
	connections map[string]*clientConnection
}{
	connections: make(map[string]*clientConnection),
}

func ProvideServer(cfg *config.Config, log *zap.Logger) *http.Server {
	mux := http.NewServeMux()
	for _, client := range cfg.Clients {
		platform := client.Platform
		mux.HandleFunc(client.Path, func(w http.ResponseWriter, r *http.Request) {
			wsHandler(w, r, platform, log)
		})
	}

	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request, platform string, log *zap.Logger) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		log.Sugar().Errorf("WebSocket 握手失败: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	log.Sugar().Infof("WebSocket 客户 端已连接 [%s]: %s", platform, r.RemoteAddr)
	client := registerClient(platform, &clientConnection{conn: conn}, log)
	defer unregisterClient(platform, client)

	ctx := context.WithValue(r.Context(), platformKey, platform)
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			log.Sugar().Errorf("WebSocket 读取已结束: %v", err)
			return
		}
		log.Sugar().Debugf("收到 [%s] 消息: %d 字节", platform, len(msg))

		// 本次分发的上传暂存区；先给出 Action，再放行上传请求获取远程写锁
		dispatchCtx := upload.BeginDispatch(ctx)
		act := event.Dispatch(dispatchCtx, platform, msg)

		// 给出 Action：非零时写回事件源客户端；零 Action 视为分发完成
		if act.Action != "" {
			resp, err := json.Marshal(act)
			if err != nil {
				log.Sugar().Warnf("序列化响应失败: %v", err)
			} else if err := client.write(ctx, resp); err != nil {
				log.Sugar().Errorf("WebSocket 写入失败: %v", err)
				// Action 写回失败（未给出），仍放行本事件上传：行为保持「所有事件仍上传」，且 Action 从未送达、无顺序违例
				log.Sugar().Warn("Action 写回失败，仍放行上传")
				upload.FinishDispatch(dispatchCtx)
				return
			}
		}
		upload.FinishDispatch(dispatchCtx)
	}
}

// SendAction 将远程回调动作转发给指定平台最新连接的客户端。
func SendAction(platform string, act action.Action) error {
	if platform == "" {
		return fmt.Errorf("回调目标平台为空")
	}
	if act.Action == "" {
		return fmt.Errorf("回调 Action 为空")
	}

	clients.RLock()
	client := clients.connections[platform]
	clients.RUnlock()
	if client == nil {
		return fmt.Errorf("平台 %s 没有已连接客户端", platform)
	}

	payload, err := json.Marshal(act)
	if err != nil {
		return fmt.Errorf("序列化回调 Action: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.write(ctx, payload); err != nil {
		return fmt.Errorf("向平台 %s 写入回调 Action: %w", platform, err)
	}
	return nil
}

func registerClient(platform string, client *clientConnection, log *zap.Logger) *clientConnection {
	clients.Lock()
	previous := clients.connections[platform]
	clients.connections[platform] = client
	clients.Unlock()
	if previous != nil {
		log.Sugar().Warnf("平台 %s 已有连接，新连接将接收远程 Action", platform)
	}
	return client
}

func unregisterClient(platform string, client *clientConnection) {
	clients.Lock()
	defer clients.Unlock()
	if clients.connections[platform] == client {
		delete(clients.connections, platform)
	}
}

func (c *clientConnection) write(ctx context.Context, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeFunc != nil {
		return c.writeFunc(ctx, payload)
	}
	if c.conn == nil {
		return fmt.Errorf("客户端连接为空")
	}
	return c.conn.Write(ctx, websocket.MessageText, payload)
}
