package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// TTSInfo 是一个 TTS 引擎的对外形态：配置里长什么样、现在能不能用。
type TTSInfo struct {
	Name string `json:"name"`
	// Enabled 表示它在 [tts].engines 的降级顺序里。
	Enabled bool `json:"enabled"`
	// Ready 表示按当前配置能构造出来（凭据与参数齐）；false 时看 Reason。
	Ready  bool   `json:"ready"`
	Voice  string `json:"voice,omitempty"`
	Model  string `json:"model,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// previewRequest 是试听请求：引擎与音色可覆盖，文本必填。
type previewRequest struct {
	Text   string `json:"text"`
	Engine string `json:"engine,omitempty"`
	Voice  string `json:"voice,omitempty"`
}

// handleTTSList 列出支持的 TTS 引擎与当前配置。
func (h *Handler) handleTTSList(w http.ResponseWriter, r *http.Request) {
	if h.TTSInfo == nil {
		writeError(w, http.StatusServiceUnavailable, "tts disabled")
		return
	}

	engines := h.TTSInfo()
	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(engines),
		"engines": engines,
	})
}

// handleTTSPreview 合成一段试听音频并直接返回（WAV）。
//
// 不入播报队列，也不经过 Sink：试听要能挑任意引擎/音色，而队列里的音色由配置决定。
// 代价是前端播放可能与正在播的内容重叠出声——放不放由前端决定。
func (h *Handler) handleTTSPreview(w http.ResponseWriter, r *http.Request) {
	if h.TTSPreview == nil {
		writeError(w, http.StatusServiceUnavailable, "tts disabled")
		return
	}

	var req previewRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "empty text")
		return
	}

	audio, err := h.TTSPreview(strings.TrimSpace(req.Engine), strings.TrimSpace(req.Voice), text)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Length", strconv.Itoa(len(audio)))
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(audio); err != nil {
		// 响应头已发出，写失败只能记日志
		logger.Warnf("写出试听音频失败: %v", err)
	}
}
