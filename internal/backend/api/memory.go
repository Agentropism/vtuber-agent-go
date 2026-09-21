package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/memory"
)

// addMemoryRequest 是一条新记忆：接口层只暴露可展示与权重字段，不碰内部下标。
type addMemoryRequest struct {
	ChannelID string `json:"channel_id,omitempty"`
	User      string `json:"user,omitempty"`
	Text      string `json:"text"`
	Reply     string `json:"reply,omitempty"`
	Weight    int    `json:"weight,omitempty"`
}

// memoryLimit 解析 limit 参数；缺省或非法时取 fallback。
func memoryLimit(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}

	return value
}

// handleMemoryList 列出记忆：带 query 时按关键词召回，不带时给最近若干条。
func (h *Handler) handleMemoryList(w http.ResponseWriter, r *http.Request) {
	if h.Memory == nil {
		writeError(w, http.StatusServiceUnavailable, "memory disabled")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))

	var entries []memory.EntryWithID
	if query != "" {
		entries = h.Memory.Recall(query, memoryLimit(r.URL.Query().Get("limit"), 5))
	} else {
		entries = h.Memory.Snapshot(memoryLimit(r.URL.Query().Get("limit"), 20))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(entries),
		"total":   h.Memory.Len(),
		"entries": entries,
	})
}

// handleMemoryAdd 追加一条记忆，返回它的 ID。
func (h *Handler) handleMemoryAdd(w http.ResponseWriter, r *http.Request) {
	if h.Memory == nil {
		writeError(w, http.StatusServiceUnavailable, "memory disabled")
		return
	}

	var req addMemoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "empty text")
		return
	}

	id, err := h.Memory.Append(memory.Entry{
		ChannelID: strings.TrimSpace(req.ChannelID),
		User:      strings.TrimSpace(req.User),
		Text:      text,
		Reply:     strings.TrimSpace(req.Reply),
		Weight:    req.Weight,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// handleMemoryDelete 删除一条记忆：写入墓碑标记，读取时过滤。
func (h *Handler) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	if h.Memory == nil {
		writeError(w, http.StatusServiceUnavailable, "memory disabled")
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid memory id")
		return
	}

	if err := h.Memory.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "id": id})
}
