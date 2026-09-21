package api

import (
	"net/http"
	"strings"
)

// StreamInfo 是推流状态：循环在不在跑、ffmpeg 推没推上、有没有卡在手机验证。
type StreamInfo struct {
	Enabled   bool `json:"enabled"`
	Running   bool `json:"running"`
	Streaming bool `json:"streaming"`
	// Output 是裁剪过的推流地址（去掉查询串里的 stream key，绝不下发凭据）。
	Output string `json:"output,omitempty"`
	// PendingCode/PendingNote 是最近一次开播被拒的业务码与原话（60024/60043/60045）。
	PendingCode int    `json:"pending_code,omitempty"`
	PendingNote string `json:"pending_note,omitempty"`
	// VerifyURL 是只允许本机访问的验证页（扫码/刷脸时用）。
	VerifyURL string `json:"verify_url,omitempty"`
}

// streamActionRequest 是启停请求体。
type streamActionRequest struct {
	Action string `json:"action"`
}

// requireStream 检查推流能力是否装配；未启用 [stream] 时返回 503。
func (h *Handler) requireStream(w http.ResponseWriter) bool {
	if h.StreamInfo == nil {
		writeError(w, http.StatusServiceUnavailable, "stream disabled")
		return false
	}

	return true
}

// handleStreamStatus 返回推流状态。
func (h *Handler) handleStreamStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requireStream(w) {
		return
	}

	writeJSON(w, http.StatusOK, h.StreamInfo())
}

// handleStreamAction 开播 / 关播。
//
// 关播会等收尾（最长约 10 秒）：不等就等于把直播间挂成「直播中」。
func (h *Handler) handleStreamAction(w http.ResponseWriter, r *http.Request) {
	if !h.requireStream(w) {
		return
	}

	var req streamActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	switch strings.TrimSpace(req.Action) {
	case "start":
		if h.StreamStart == nil {
			writeError(w, http.StatusServiceUnavailable, "stream start unavailable")
			return
		}
		if err := h.StreamStart(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	case "stop":
		if h.StreamStop == nil {
			writeError(w, http.StatusServiceUnavailable, "stream stop unavailable")
			return
		}
		if err := h.StreamStop(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "action 必须是 start 或 stop")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"stream": h.StreamInfo(),
	})
}
