package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// toolCallTimeout 是直接调用工具的时间上限：工具是本地实现，正常都很快。
const toolCallTimeout = 30 * time.Second

// toolView 是工具的对外形态：定义 + 是否允许直接调用。
type toolView struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	ReadOnly    bool            `json:"read_only"`
}

// handleToolsList 列出已注册的工具。
func (h *Handler) handleToolsList(w http.ResponseWriter, r *http.Request) {
	if h.Tools == nil {
		writeError(w, http.StatusServiceUnavailable, "tools disabled")
		return
	}

	views := make([]toolView, 0, len(h.Tools.Names()))
	for _, name := range h.Tools.Names() {
		def, ok := h.Tools.Def(name)
		if !ok {
			continue
		}
		views = append(views, toolView{
			Name:        def.Name,
			Description: def.Description,
			Parameters:  def.Parameters,
			ReadOnly:    h.Tools.IsReadOnly(name),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(views),
		"tools": views,
	})
}

// toolCallRequest 是直接调用的请求体：args 直接喂给工具，与模型给的参数同构。
type toolCallRequest struct {
	Args json.RawMessage `json:"args,omitempty"`
}

// handleToolCall 直接执行一个工具。
//
// 只放行只读工具：/api/* 不鉴权（见 docs/API.md），把写记忆这类有副作用的工具
// 开给局域网等于把数据面也开了。要放开得先在注册处标记只读，是显式的动作。
func (h *Handler) handleToolCall(w http.ResponseWriter, r *http.Request) {
	if h.Tools == nil {
		writeError(w, http.StatusServiceUnavailable, "tools disabled")
		return
	}

	name := r.PathValue("name")
	if _, ok := h.Tools.Def(name); !ok {
		writeError(w, http.StatusNotFound, "unknown tool: "+name)
		return
	}
	if !h.Tools.IsReadOnly(name) {
		writeError(w, http.StatusForbidden, "tool has side effects and is not directly callable")
		return
	}

	var req toolCallRequest
	if r.Body != nil {
		if !decodeJSON(w, r, &req) {
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), toolCallTimeout)
	defer cancel()

	result, err := h.Tools.Call(ctx, name, req.Args)
	if err != nil {
		logger.Warnf("工具直接调用失败: %s: %v", name, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Infof("工具直接调用完成: %s", name)
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "result": result})
}
