package api

import (
	"net/http"
	"strings"

	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/upload"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/event"
)

// 适配器标识：决定渠道号前缀对应哪个平台。平台名本身取自 [[clients]].platform，
// 所以自定义平台名（platform = "qq2"）也能工作。
const (
	adapterOneBot       = "onebot_v11"
	adapterBilibiliLive = "bilibili_live"
)

// historyMessage 是历史消息的对外形态：只暴露可展示的字段。
type historyMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// sendMessageRequest 是「以观众身份发一条消息」的请求体。
type sendMessageRequest struct {
	UserID   string `json:"user_id,omitempty"`
	UserName string `json:"user_name,omitempty"`
	Text     string `json:"text"`
}

// handleSessions 列出当前所有渠道会话的快照。
func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !h.requireSessions(w) {
		return
	}

	channels := h.Sessions.Channels()
	writeJSON(w, http.StatusOK, map[string]any{
		"count":    len(channels),
		"sessions": channels,
	})
}

// handleHistory 返回指定渠道的历史消息。
func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request) {
	if !h.requireSessions(w) {
		return
	}

	channelID := r.PathValue("id")

	history, ok := h.Sessions.History(channelID)
	if !ok {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	messages := make([]historyMessage, 0, len(history))
	for _, message := range history {
		messages = append(messages, historyMessage{Role: message.Role, Content: message.Content})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"channel_id": channelID,
		"messages":   messages,
	})
}

// handleSendMessage 以观众身份发一条消息。
//
// 它经与平台事件同一条上传管线（去重、敏感词、背压一个不落），因此行为与真实弹幕一致：
// 会进会话、会生成回复、会写记忆。这是接口层的「模拟观众事件」入口，不是第二条会话通道。
func (h *Handler) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if !h.requireSessions(w) {
		return
	}
	if !upload.Ready() {
		writeError(w, http.StatusServiceUnavailable, "event pipeline has no handler")
		return
	}

	channelID := r.PathValue("id")

	var req sendMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "empty text")
		return
	}

	if h.Config == nil {
		writeError(w, http.StatusServiceUnavailable, "config unavailable")
		return
	}
	cfg, err := h.Config()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	platform, channelType, ok := channelBinding(cfg, channelID)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown channel: 需要形如 group_xxx 或 room_xxx 的渠道号，且平台已在 [[clients]] 配置")
		return
	}

	userName := strings.TrimSpace(req.UserName)
	if userName == "" {
		userName = "接口调试"
	}

	if err := upload.UploadSimulatedChat(r.Context(), upload.SimulatedChat{
		Platform:    platform,
		ChannelID:   channelID,
		ChannelType: channelType,
		UserID:      strings.TrimSpace(req.UserID),
		UserName:    userName,
		Text:        text,
	}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// channelBinding 把渠道号还原成平台与渠道类型。
//
// 渠道号前缀是既有约定（event.ChannelPrefixGroup / ChannelPrefixRoom），平台名则回查
// [[clients]]：没配过的平台不该凭空造出会话，那只会得到一个永远没有出口的幽灵会话。
func channelBinding(cfg *config.Config, channelID string) (platform, channelType string, ok bool) {
	var adapterKey string

	switch {
	case strings.HasPrefix(channelID, event.ChannelPrefixGroup):
		adapterKey, channelType = adapterOneBot, "group"
	case strings.HasPrefix(channelID, event.ChannelPrefixRoom):
		adapterKey, channelType = adapterBilibiliLive, "live_room"
	default:
		return "", "", false
	}

	for _, client := range cfg.Clients {
		if client.AdapterKey == adapterKey {
			return client.Platform, channelType, true
		}
	}

	return "", "", false
}
