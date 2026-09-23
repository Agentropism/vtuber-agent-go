package api

import (
	"net/http"

	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
)

// handleConfig 返回当前生效的配置快照。
//
// 响应结构与 config.toml 同构（键名取自 toml tag），密钥类字段替换成
// {"configured": bool}——前端据此展示「有没有配」，明文永远不出后端。
//
// 脱敏本身在 core/config.RedactedMap：命令行 config show 用的是同一份实现，
// 这里不再自己渲染一遍。
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if h.Config == nil {
		writeError(w, http.StatusServiceUnavailable, "config unavailable")
		return
	}

	cfg, err := h.Config()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, config.RedactedMap(cfg))
}
