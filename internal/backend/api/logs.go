package api

import (
	"net/http"
	"strconv"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// handleLogs 返回进程内最近的日志（环形缓冲，新的在前）。
//
// level 是「不低于该级别」的过滤：debug / info / warn / error；
// 不传或未知级别时不过滤。
func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if h.Logs == nil {
		writeError(w, http.StatusServiceUnavailable, "logs unavailable")
		return
	}

	limit := logger.DefaultQueryLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = value
	}

	entries := h.Logs(limit, r.URL.Query().Get("level"))
	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(entries),
		"entries": entries,
	})
}
