package api

import (
	"net/http"
	"time"
)

// handleStatus 汇总运行状态：谁连着、播报队列跑到哪、上报管线堵不堵。
//
// 只读聚合，不含敏感信息（配置带凭据的字段在 /api/config 里已脱敏，这里根本不碰配置）。
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{
		"uptime": time.Since(h.Started).Round(time.Second).String(),
	}
	if !h.Started.IsZero() {
		status["started_at"] = h.Started.UTC().Format(time.RFC3339)
	}

	if h.Clients != nil {
		platforms := h.Clients()
		status["clients"] = map[string]any{
			"count":     len(platforms),
			"platforms": platforms,
		}
	}

	if h.Queue != nil {
		stats := h.Queue.Stats()
		status["broadcast"] = map[string]uint64{
			"enqueued":  stats.Enqueued,
			"played":    stats.Played,
			"preempted": stats.Preempted,
			"dropped":   stats.Dropped,
			"failed":    stats.Failed,
		}
	}

	if h.Upload != nil {
		stats := h.Upload()
		status["upload"] = map[string]any{
			"queue_len":  stats.QueueLen,
			"queue_size": stats.QueueSize,
			"dropped":    stats.Dropped,
		}
	}

	if h.Sessions != nil {
		status["sessions"] = len(h.Sessions.Channels())
	}

	writeJSON(w, http.StatusOK, status)
}
