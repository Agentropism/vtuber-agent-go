package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// maxInjectBody 是 /inject 请求体上限，避免异常请求占用内存。
const maxInjectBody = 64 << 10

// InjectRequest 是 POST /inject 的请求体：让网关把一段文本念出来。
//
// 只保留内容字段——要念的文本与要看的表情。入队即返回，没有播放完成确认；
// 语速、音量、音色由 TTS 引擎配置决定，请求里不带这些参数。
type InjectRequest struct {
	Text string `json:"text"`
	// Emotion 是 Live2D 表情标签（模型 emo_map 的英文名，如 "joy"），
	// 空表示不换表情。取值合法性由模型决定，本包不校验，原样透传。
	Emotion string `json:"emotion,omitempty"`
}

// InjectFunc 处理一次注入；返回错误表示没有入队。
type InjectFunc func(InjectRequest) error

var (
	injectMu   sync.RWMutex
	injectFunc InjectFunc
)

// SetInjectHandler 注入 /inject 的实现，由 app 装配时调用。
//
// 与 upload.SetHandler 同一套路：接入层只认请求形态，「交给谁」由装配层决定，
// 这样 gateway 不必反向依赖 agent。未注入时接口返回 503。
func SetInjectHandler(fn InjectFunc) {
	injectMu.Lock()
	injectFunc = fn
	injectMu.Unlock()
}

func currentInjectFunc() InjectFunc {
	injectMu.RLock()
	defer injectMu.RUnlock()
	return injectFunc
}

// handleInject 处理 POST /inject：校验请求体后交给注入实现。
func handleInject(w http.ResponseWriter, r *http.Request, log *zap.Logger) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"}, log)
		return
	}

	var req InjectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxInjectBody)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"}, log)
		return
	}

	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty text"}, log)
		return
	}

	fn := currentInjectFunc()
	if fn == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "broadcast disabled"}, log)
		return
	}

	if err := fn(req); err != nil {
		log.Sugar().Warnf("播报注入未入队: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()}, log)
		return
	}

	log.Sugar().Infof("播报注入已入队: emotion=%q 文本=%s", req.Emotion, req.Text)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}, log)
}

// writeJSON 写出 JSON 响应；走到这里响应头已发出，序列化失败只能记日志。
func writeJSON(w http.ResponseWriter, status int, body any, log *zap.Logger) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Sugar().Warnf("写出响应失败: %v", err)
	}
}
