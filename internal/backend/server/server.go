package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/event"
	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/upload"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/action"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
	"github.com/coder/websocket"
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

// Route 是额外挂到网关 mux 上的路由（前端页面、/client-ws、模型静态资源等）。
//
// 由 app 装配时传入：接入层只负责挂载，不关心它背后是什么实现，
// 这样 gateway 不必反向依赖 agent。
type Route struct {
	Pattern string
	Handler http.Handler
}

func ProvideServer(cfg *config.Config, extra ...Route) *http.Server {
	mux := http.NewServeMux()
	patterns := make(map[string]string, len(cfg.Clients))

	for _, client := range cfg.Clients {
		platform := client.Platform
		if previous, ok := patterns[client.Path]; ok {
			logger.Errorf("接入路径 %s 被平台 %s 与 %s 重复占用，跳过后者", client.Path, previous, platform)
			continue
		}
		patterns[client.Path] = platform
		mux.HandleFunc(client.Path, func(w http.ResponseWriter, r *http.Request) {
			wsHandler(w, r, platform)
		})
	}

	// /inject 必须显式注册：客户端路径可以配成 "/"，会兜住所有路径，
	// 否则注入请求会被当成 WebSocket 握手（405），而不是拿到明确的状态码。
	mux.HandleFunc("/inject", func(w http.ResponseWriter, r *http.Request) {
		handleInject(w, r)
	})
	patterns["/inject"] = "inject"

	// 额外路由：与接入路径冲突时跳过并报错，而不是让 http.ServeMux panic
	// （重复注册同一个 pattern 会直接 panic，把整个进程带走）。
	for _, route := range extra {
		if previous, ok := patterns[route.Pattern]; ok {
			logger.Errorf("路由 %s 与 %s 冲突，跳过该额外路由", route.Pattern, previous)
			continue
		}
		patterns[route.Pattern] = "额外路由"
		mux.Handle(route.Pattern, route.Handler)
	}

	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request, platform string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		logger.Errorf("WebSocket 握手失败: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	logger.Infof("WebSocket 客户 端已连接 [%s]: %s", platform, r.RemoteAddr)
	client := registerClient(platform, &clientConnection{conn: conn})
	defer unregisterClient(platform, client)

	ctx := context.WithValue(r.Context(), platformKey, platform)
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			// 客户端正常挂断是常态（重启、断网），不该按错误打堆栈刷屏
			if status := websocket.CloseStatus(err); status == websocket.StatusNormalClosure ||
				status == websocket.StatusGoingAway {
				logger.Infof("WebSocket 客户端已断开 [%s]", platform)
				return
			}
			logger.Errorf("WebSocket 读取已结束: %v", err)
			return
		}
		logger.Debugf("收到 [%s] 消息: %d 字节", platform, len(msg))

		// 本次分发的上传暂存区；先给出 Action，再放行上传请求获取远程写锁
		dispatchCtx := upload.BeginDispatch(ctx)
		act := event.Dispatch(dispatchCtx, platform, msg)

		// 给出 Action：非零时写回事件源客户端；零 Action 视为分发完成
		if act.Action != "" {
			resp, err := json.Marshal(act)
			if err != nil {
				logger.Warnf("序列化响应失败: %v", err)
			} else if err := client.write(ctx, resp); err != nil {
				logger.Errorf("WebSocket 写入失败: %v", err)
				// Action 写回失败（未给出），仍放行本事件上传：行为保持「所有事件仍上传」，且 Action 从未送达、无顺序违例
				logger.Warn("Action 写回失败，仍放行上传")
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

func registerClient(platform string, client *clientConnection) *clientConnection {
	clients.Lock()
	previous := clients.connections[platform]
	clients.connections[platform] = client
	clients.Unlock()
	if previous != nil {
		logger.Warnf("平台 %s 已有连接，新连接将接收远程 Action", platform)
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
