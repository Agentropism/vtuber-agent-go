package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
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

	// 先收集、再注册：根路径可能同时被接入客户端（[[clients]].path = "/"）与前端页面占用，
	// 而 ServeMux 对同一个 pattern 只允许注册一次（重复注册直接 panic）。冲突必须在注册前解决。
	platforms := make(map[string]string, len(cfg.Clients)) // pattern → 平台名
	order := make([]string, 0, len(cfg.Clients))

	for _, client := range cfg.Clients {
		if previous, ok := platforms[client.Path]; ok {
			logger.Errorf("接入路径 %s 被平台 %s 与 %s 重复占用，跳过后者", client.Path, previous, client.Platform)
			continue
		}
		platforms[client.Path] = client.Platform
		order = append(order, client.Path)
	}

	// 接口层与页面都是 extra 路由（由 app 从 api 包与 web 包挂上来）。
	// 客户端路径可以配成 "/" 兜住所有路径，但 ServeMux 按最长前缀匹配，
	// /api/* 与 /favicon.ico 这类更具体的 pattern 仍会落到自己的处理函数上。
	var mergedRoot http.Handler // 非 nil：根路径上页面与接入端共存，按是否升级分流
	pending := make([]Route, 0, len(extra))

	for _, route := range extra {
		platform, conflict := platforms[route.Pattern]
		if !conflict {
			pending = append(pending, route)
			continue
		}
		if route.Pattern == rootPath {
			// 页面要挂在 "/"、适配端也把 path 配成 "/"：合成一个分流处理函数，
			// 而不是把页面丢掉（升级请求走接入端，其余走页面）。
			mergedRoot = dispatchRoot(platform, route.Handler)
			continue
		}

		logger.Errorf("路由 %s 与 %s 冲突，跳过该额外路由", route.Pattern, platform)
	}

	// 接入路径
	for _, pattern := range order {
		platform := platforms[pattern]
		if pattern == rootPath && mergedRoot != nil {
			mux.Handle(pattern, mergedRoot)
			continue
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			wsHandler(w, r, platform)
		})
	}

	// 额外路由：与接入路径冲突的已在上一步剔除
	for _, route := range pending {
		mux.Handle(route.Pattern, route.Handler)
	}

	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// rootPath 是根路径：前端页面与接入客户端都可能占它。
const rootPath = "/"

// dispatchRoot 合成根路径的处理函数：WebSocket 升级走接入端，其余请求走额外路由（前端页面）。
//
// 判据是 Upgrade 头，不需要真的握手——非升级请求交给页面，浏览器才打得开首页。
func dispatchRoot(platform string, extra http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.Header.Get("Upgrade")), "websocket") {
			wsHandler(w, r, platform)
			return
		}

		extra.ServeHTTP(w, r)
	})
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

// ConnectedPlatforms 返回当前有连接在线的平台名，按字典序排列。
//
// 只读快照，供观测接口（/api/status）展示接入端在线情况。
func ConnectedPlatforms() []string {
	clients.RLock()
	platforms := make([]string, 0, len(clients.connections))
	for platform := range clients.connections {
		platforms = append(platforms, platform)
	}
	clients.RUnlock()

	sort.Strings(platforms)

	return platforms
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
