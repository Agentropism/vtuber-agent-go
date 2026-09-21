// Package api 是 backend 对前端暴露的 HTTP 接口层：配置只读、播报注入、会话读写、运行状态。
//
// 分工：请求-响应走这里的 REST 接口，推送（播报下发、播放回执）仍走 web 包的 /client-ws。
// 依赖全部由 app 装配时注入——接口层不关心配置从哪读、状态怎么算；未装配的能力用 nil
// 表示，对应接口返回 503，而不是假装成功。
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/backend/server"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation"
	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/upload"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// Handler 汇总接口层所需的依赖。
type Handler struct {
	// Config 每次调用都重读磁盘，与 config.ProvideConfig 的「允许运行时改配置」一致。
	Config func() (*config.Config, error)
	// Speak 把文本与表情交给播报队列；nil 表示播报链路未装配（未配 [tts].engines）。
	Speak func(text, emotion string) error
	// Sessions 是会话层；nil 表示未配置 [llm]，此时没有会话可读也没有终端处理器。
	Sessions *conversation.Sessions
	// Queue 提供播报队列计数；nil 表示播报链路未装配。
	Queue *broadcast.Queue
	// Clients 返回在线接入平台；nil 表示取不到。
	Clients func() []string
	// Upload 返回上报管线快照；nil 表示取不到。
	Upload func() upload.PipelineStats
	// Started 是进程启动时间，用于算运行时长。
	Started time.Time
}

// Routes 返回挂在网关 mux 上的接口路由。
//
// 路径用 Go 1.22 的方法前缀与通配符写法，方法与子路径交给 mux 分派。
func (h *Handler) Routes() []server.Route {
	return []server.Route{
		{Pattern: "GET /api/config", Handler: http.HandlerFunc(h.handleConfig)},
		{Pattern: "POST /api/speak", Handler: http.HandlerFunc(h.handleSpeak)},
		{Pattern: "GET /api/sessions", Handler: http.HandlerFunc(h.handleSessions)},
		{Pattern: "GET /api/sessions/{id}/history", Handler: http.HandlerFunc(h.handleHistory)},
		{Pattern: "POST /api/sessions/{id}/messages", Handler: http.HandlerFunc(h.handleSendMessage)},
		{Pattern: "GET /api/status", Handler: http.HandlerFunc(h.handleStatus)},
	}
}

// maxBody 是请求体上限，避免异常请求占用内存。
const maxBody = 64 << 10

// writeJSON 写出 JSON 响应；走到这里响应头已发出，序列化失败只能记日志。
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Warnf("写出响应失败: %v", err)
	}
}

// writeError 是最常见的错误响应形态。
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// decodeJSON 解析请求体；失败时已写好 400 响应，返回 false。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return false
	}

	return true
}

// requireSessions 检查会话层是否可用；不可用时已写好 503 响应，返回 false。
func (h *Handler) requireSessions(w http.ResponseWriter) bool {
	if h.Sessions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent sessions disabled")
		return false
	}

	return true
}
