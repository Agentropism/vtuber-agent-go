package api

import (
	"net/http"
	"strings"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// speakRequest 是 POST /api/speak（旧路径 /inject）的请求体：让网关把一段文本念出来。
//
// 只带内容字段——要念的文本与要看的表情。语速、音量、音色由 TTS 配置决定，请求里不带。
type speakRequest struct {
	Text string `json:"text"`
	// Emotion 是 Live2D 表情标签（模型 emo_map 的英文名，如 joy），空表示不换表情。
	// 取值是否可用由前端加载的模型决定，这里不校验、原样透传。
	Emotion string `json:"emotion,omitempty"`
}

// handleSpeak 处理播报注入：入队即返回，不等待合成、也不等待播完。
func (h *Handler) handleSpeak(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req speakRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "empty text")
		return
	}

	if h.Speak == nil {
		writeError(w, http.StatusServiceUnavailable, "broadcast disabled")
		return
	}

	if err := h.Speak(text, req.Emotion); err != nil {
		logger.Warnf("播报注入未入队: %v", err)
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	logger.Infof("播报注入已入队: emotion=%q 文本=%s", req.Emotion, text)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
