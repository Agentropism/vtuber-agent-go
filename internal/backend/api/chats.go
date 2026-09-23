package api

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/archive"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// handleChats 列出归档的平台与渠道摘要。
func (h *Handler) handleChats(w http.ResponseWriter, r *http.Request) {
	if h.Chats == nil {
		writeError(w, http.StatusServiceUnavailable, "archive disabled")
		return
	}

	platforms, err := h.Chats.Channels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"platforms": platforms})
}

// handleChatRecords 返回某渠道的归档记录（新的在前，limit 默认 100，before 为 RFC3339）。
func (h *Handler) handleChatRecords(w http.ResponseWriter, r *http.Request) {
	if h.Chats == nil {
		writeError(w, http.StatusServiceUnavailable, "archive disabled")
		return
	}

	platform := r.PathValue("platform")
	channel := r.PathValue("channel")

	limit := archive.DefaultReadLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = value
	}

	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before")
			return
		}
		before = parsed
	}

	records, found, err := h.Chats.Read(platform, channel, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"platform":   platform,
		"channel_id": channel,
		"count":      len(records),
		"records":    records,
	})
}

// handleChatsExport 导出归档为文件下载：format 取 jsonl（默认）或 md；
// 不带 channel 时导出整个分类。
func (h *Handler) handleChatsExport(w http.ResponseWriter, r *http.Request) {
	if h.Chats == nil {
		writeError(w, http.StatusServiceUnavailable, "archive disabled")
		return
	}

	query := r.URL.Query()
	platform := strings.TrimSpace(query.Get("platform"))
	channel := strings.TrimSpace(query.Get("channel"))
	if platform == "" {
		writeError(w, http.StatusBadRequest, "empty platform")
		return
	}

	format := strings.ToLower(strings.TrimSpace(query.Get("format")))
	if format == "" {
		format = "jsonl"
	}
	if format != "jsonl" && format != "md" {
		writeError(w, http.StatusBadRequest, "unsupported format")
		return
	}

	// 先在内存里导出：渠道不存在时才能干净地回 404，而不是写了一半才发现
	var buffer bytes.Buffer
	found, err := h.Chats.Export(platform, channel, format, &buffer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	contentType := "application/x-ndjson; charset=utf-8"
	if format == "md" {
		contentType = "text/markdown; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", exportFileName(platform, channel, format)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buffer.Bytes()); err != nil {
		logger.Warnf("写出导出内容失败: %v", err)
	}
}

// exportFileName 拼出下载文件名；用户输入只保留安全字符，防止响应头注入。
func exportFileName(platform, channel, format string) string {
	name := "chat"
	for _, part := range []string{platform, channel} {
		if token := safeFileToken(part); token != "" {
			name += "_" + token
		}
	}

	extension := "jsonl"
	if format == "md" {
		extension = "md"
	}

	return name + "." + extension
}

// safeFileToken 只保留字母、数字、下划线与连字符。
func safeFileToken(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			builder.WriteRune(r)
		}
	}

	return builder.String()
}
