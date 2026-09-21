package api

import (
	"net/http"
	"strings"
)

// ModelInfo 是模型信息的对外形态。
type ModelInfo struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Scale       float64  `json:"scale"`
	XShift      float64  `json:"x_shift,omitempty"`
	YShift      float64  `json:"y_shift,omitempty"`
	IdleMotion  string   `json:"idle_motion,omitempty"`
	Expressions []string `json:"expressions,omitempty"`
}

// ModelState 是模型相关的状态：当前模型、可选清单、当前表情标签。
type ModelState struct {
	Current  ModelInfo   `json:"current"`
	Models   []ModelInfo `json:"models"`
	Emotions []string    `json:"emotions"`
}

// switchModelRequest 是切换模型的请求体。
type switchModelRequest struct {
	Name string `json:"name"`
}

// emotionRequest 是表情预览的请求体。
type emotionRequest struct {
	Label string `json:"label"`
}

// requireModels 检查模型能力是否装配；不可用时已写好 503 响应。
func (h *Handler) requireModels(w http.ResponseWriter) bool {
	if h.ModelState == nil || h.ModelSwitch == nil {
		writeError(w, http.StatusServiceUnavailable, "frontend disabled")
		return false
	}

	return true
}

// handleModels 返回当前模型、可选模型与表情标签。
func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	if !h.requireModels(w) {
		return
	}

	state, err := h.ModelState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, state)
}

// handleModelSwitch 热切换模型：重建清单与表情词表，并给已连前端补发 hello。
func (h *Handler) handleModelSwitch(w http.ResponseWriter, r *http.Request) {
	if !h.requireModels(w) {
		return
	}

	var req switchModelRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "empty model name")
		return
	}

	// 先按可用清单校验：切到不存在的模型要回 404，而不是把内部错误原样抛出去
	if err := h.ensureModelAvailable(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if err := h.ModelSwitch(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	state, err := h.ModelState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "current": state.Current})
}

// ensureModelAvailable 确认模型在可用清单里。
func (h *Handler) ensureModelAvailable(name string) error {
	state, err := h.ModelState()
	if err != nil {
		return err
	}

	for _, candidate := range state.Models {
		if candidate.Name == name {
			return nil
		}
	}

	return errUnknownModel(name)
}

// unknownModelError 让 404 的文案带上模型名。
type unknownModelError string

func (e unknownModelError) Error() string { return "unknown model: " + string(e) }

func errUnknownModel(name string) error { return unknownModelError(name) }

// handleEmotionPreview 让前端预览一个表情：标签换成表达式下标后下发。
func (h *Handler) handleEmotionPreview(w http.ResponseWriter, r *http.Request) {
	if h.EmotionShow == nil {
		writeError(w, http.StatusServiceUnavailable, "frontend disabled")
		return
	}

	var req emotionRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		writeError(w, http.StatusBadRequest, "empty emotion label")
		return
	}

	index, err := h.EmotionShow(label)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"label": label, "emotion": index})
}
