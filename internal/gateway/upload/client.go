package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/gateway/event/bilibililive"
	onebot "github.com/Agentropism/vtuber-agent-go/internal/gateway/event/onebot"
	wordfilter "github.com/Agentropism/vtuber-agent-go/internal/gateway/filter"
	"github.com/Agentropism/vtuber-agent-go/internal/shared/event"

	"go.uber.org/zap"
)

var log = zap.NewNop()

// 事件类型常量
const (
	EventTypeMessage = "message"
	EventTypeNotice  = "notice"
)

type platformEvent struct {
	Type string `json:"type"`
	// Kind 是比 Type 更细的事件种类：礼物、醒目留言、大航海在协议上都属于
	// message，下游需要靠它分辨来源，进而决定播报优先级与是否需要回复。
	Kind           event.Kind        `json:"event_kind"`
	PlatformName   string            `json:"platform_name"`
	ChannelID      string            `json:"channel_id"`
	ChannelName    string            `json:"channel_name"`
	ChannelType    string            `json:"channel_type"`
	ChannelAvatar  string            `json:"channel_avatar"`
	UserID         string            `json:"user_id"`
	UserName       string            `json:"user_name"`
	UserAvatar     string            `json:"user_avatar"`
	MessageID      string            `json:"message_id"`
	SenderID       string            `json:"sender_id"`
	SenderName     string            `json:"sender_name"`
	SenderNickname string            `json:"sender_nickname"`
	SenderAvatar   string            `json:"sender_avatar"`
	ContentData    []platformContent `json:"content_data"`
	ContentText    string            `json:"content_text"`
	IsToMe         bool              `json:"is_tome"`
	Timestamp      int64             `json:"timestamp"`
	IsSelf         bool              `json:"is_self"`
	RefChatKey     string            `json:"ref_chat_key"`
	RefMsgID       string            `json:"ref_msg_id"`
	RefSenderID    string            `json:"ref_sender_id"`
}

type platformContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ---- 管线状态 ----

var (
	queue            chan []byte // 有界缓存，Init 时按 QueueSize 创建
	done             = make(chan struct{})
	queueWaitTimeout time.Duration // 缓存满时入队等待上限，0=无限等待
	filter           *wordfilter.Filter

	// lifecycleMu 串行化 Init/Shutdown 与包级状态重写，防止与前一生命周期遗留的消费协程竞争
	lifecycleMu sync.Mutex

	// handler 事件终端处理器，由 app 注入（本地 agent 会话层）。
	handlerMu sync.RWMutex
	handler   Handler
)

// Handler 是上传管线的终端处理器。
//
// 它在专用消费协程中串行调用，不应长时间阻塞——阻塞会拖慢整条管线并触发背压。
// 典型实现是本地 agent 会话层的 Handle：解析事件、路由到会话、异步生成回复。
type Handler func(payload []byte)

// SetHandler 注入事件终端处理器，可在 Init 前后任意时刻调用。
func SetHandler(h Handler) {
	handlerMu.Lock()
	handler = h
	handlerMu.Unlock()
}

// currentHandler 返回当前处理器，未注入时为 nil。
func currentHandler() Handler {
	handlerMu.RLock()
	defer handlerMu.RUnlock()
	return handler
}

// Options 上传管线配置。
type Options struct {
	QueueSize          int           // 缓存容量，<=0 按 256 处理
	QueueWaitTimeout   time.Duration // 缓存满时等待上限，0=无限等待
	SensitiveWordsFile string        // 敏感词库文件路径，空=不启用
	DedupTTL           time.Duration // 去重窗口，0=不启用
}

// DispatchScope 一次事件分发期间暂存的上传请求。
// 请求在 FinishDispatch 之前不允许进入缓存，保证「先给出 Action 再上传」。
type DispatchScope struct {
	staged [][]byte // 已序列化的待上传负载
}

type scopeKey struct{}

func SetLogger(l *zap.Logger) {
	if l != nil {
		log = l
	}
}

// Init 初始化上传管线：创建有界缓存与过滤器，并启动消费协程。
//
// 事件终端由 SetHandler 注入（本地 agent 会话层），未注入时事件会被丢弃。
// opts 为上传管线配置。
func Init(opts Options) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()

	// 先关闭旧 done：保证前一生命周期遗留的消费协程观察到关闭信号后退出，
	// 再重写包级状态，避免与读 `<-done` 的旧协程竞争
	select {
	case <-done:
	default:
		close(done)
	}

	size := opts.QueueSize
	if size <= 0 {
		size = 256
	}
	queue = make(chan []byte, size)
	done = make(chan struct{})
	queueWaitTimeout = opts.QueueWaitTimeout

	f, err := wordfilter.New(opts.SensitiveWordsFile, opts.DedupTTL)
	if err != nil {
		return fmt.Errorf("创建敏感词过滤器: %w", err)
	}
	filter = f

	go consumeLoop(queue, done)
	return nil
}

// Shutdown 关闭长连接和后台协程。
func Shutdown() {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()

	select {
	case <-done:
	default:
		close(done)
	}
}

// consumeLoop 串行消费缓存中的事件并交给终端处理器。
//
// pending/stop 为 Init 持锁时传入的快照：循环内只读快照，杜绝与后续 Init
// 在锁内重写 queue/done 竞争。
func consumeLoop(pending <-chan []byte, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case payload := <-pending:
			h := currentHandler()
			if h == nil {
				log.Sugar().Warn("未注入事件处理器，丢弃事件")
				continue
			}
			h(payload)
		}
	}
}

// ---- 公共入口 ----

// Upload 异步上传群消息事件。
func Upload(ctx context.Context, platform string, e onebot.MessageGroupEvent) {
	upload(ctx, buildPlatformEvent(platform, e))
}

// UploadBilibiliDM 异步上传 B 站直播弹幕事件。
func UploadBilibiliDM(ctx context.Context, platform string, e bilibililive.LiveOpenPlatformDMEvent) {
	upload(ctx, buildBilibiliDMPlatformEvent(platform, e))
}

// UploadBilibiliGift 异步上传 B 站礼物事件。
func UploadBilibiliGift(ctx context.Context, platform string, e bilibililive.LiveOpenPlatformSendGiftEvent) {
	upload(ctx, buildBilibiliGiftPlatformEvent(platform, e))
}

// UploadBilibiliSuperChat 异步上传 B 站醒目留言事件。
func UploadBilibiliSuperChat(ctx context.Context, platform string, e bilibililive.LiveOpenPlatformSuperChatEvent) {
	upload(ctx, buildBilibiliSuperChatPlatformEvent(platform, e))
}

// UploadBilibiliGuard 异步上传 B 站大航海事件。
func UploadBilibiliGuard(ctx context.Context, platform string, e bilibililive.LiveOpenPlatformGuardEvent) {
	upload(ctx, buildBilibiliGuardPlatformEvent(platform, e))
}

// UploadBilibiliNotice 异步上传使用 open_id 标识用户的 B 站通知事件。
//
// kind 必须是该通知对应的种类：点赞 / 进入直播间 / 开播 / 下播。
func UploadBilibiliNotice(ctx context.Context, platform string, kind event.Kind, openID, uname, text string) {
	upload(ctx, buildBilibiliNoticePlatformEvent(platform, kind, openID, uname, text))
}

// UploadNotice 异步上传群通知事件。
func UploadNotice(ctx context.Context, platform string, userID int64, text string) {
	upload(ctx, buildNoticePlatformEvent(platform, userID, text))
}

// ---- 内部 ----

// BeginDispatch 创建本次事件分发的上传暂存区，并挂到返回的 ctx 上。
// 分发期间 handler 内的 upload 调用将请求暂存，等待 FinishDispatch 放行。
func BeginDispatch(ctx context.Context) context.Context {
	return context.WithValue(ctx, scopeKey{}, &DispatchScope{})
}

// FinishDispatch 放行本次分发暂存的上传请求。
// 调用方必须保证此时 Action 已经给出（已写回事件源客户端，或分发完成）。
func FinishDispatch(ctx context.Context) {
	scope, ok := ctx.Value(scopeKey{}).(*DispatchScope)
	if !ok || scope == nil {
		return
	}
	for _, payload := range scope.staged {
		enqueue(payload)
	}
	scope.staged = nil
}

// scopeFrom 从 ctx 取本次分发的暂存区；没有则返回 nil。
func scopeFrom(ctx context.Context) *DispatchScope {
	scope, _ := ctx.Value(scopeKey{}).(*DispatchScope)
	return scope
}

// enqueue 将负载写入有界缓存。
// 缓存满时按 queueWaitTimeout 等待（0=无限等待），实现背压；超时则丢弃并记录错误。
func enqueue(payload []byte) {
	// 未注入终端处理器时无消费方：非阻塞丢弃，避免无限等待挂起事件链
	if currentHandler() == nil {
		log.Sugar().Warn("未注入事件处理器，丢弃事件")
		return
	}
	// done 已关闭时直接丢弃，避免 select 在关闭与未满之间随机落到入队分支
	select {
	case <-done:
		log.Sugar().Warn("网关已关闭，丢弃事件")
		return
	default:
	}
	if queueWaitTimeout <= 0 {
		select {
		case queue <- payload:
			log.Sugar().Debug("上传事件已入队")
		case <-done:
			log.Sugar().Warn("上传缓存已满且网关关闭，丢弃事件")
		}
		return
	}
	timer := time.NewTimer(queueWaitTimeout)
	defer timer.Stop()
	select {
	case queue <- payload:
		log.Sugar().Debug("上传事件已入队")
	case <-timer.C:
		log.Sugar().Error("上传缓存已满且等待超时，丢弃事件")
	case <-done:
		log.Sugar().Warn("上传缓存已满且网关关闭，丢弃事件")
	}
}

func upload(ctx context.Context, e platformEvent) {
	// 层0：最前 —— 去重 + 敏感词过滤
	if filter != nil {
		key := e.PlatformName + ":" + e.ChannelID + ":" + e.MessageID
		if e.MessageID != "" && filter.Duplicate(key) {
			return
		}
		if filter.Sensitive(e.ContentText) {
			log.Sugar().Warnf("消息命中敏感词已丢弃: key=%s", key)
			return
		}
	}

	payload, err := json.Marshal(e)
	if err != nil {
		log.Sugar().Errorf("序列化平台事件失败: %v", err)
		return
	}

	// 分发期间：暂存，等待 Action 给出后放行；无分发上下文：直接入队
	if scope := scopeFrom(ctx); scope != nil {
		scope.staged = append(scope.staged, payload)
		return
	}
	enqueue(payload)
}

func buildPlatformEvent(platform string, e onebot.MessageGroupEvent) platformEvent {
	userName := e.Sender.Card
	if userName == "" {
		userName = e.Sender.Nickname
	}
	if userName == "" {
		userName = strconv.FormatInt(e.UserID, 10)
	}

	channelName := e.GroupName
	if channelName == "" {
		channelName = fmt.Sprintf("group_%d", e.GroupID)
	}

	senderID := strconv.FormatInt(e.UserID, 10)
	messageID := strconv.FormatInt(e.MessageID, 10)
	channelID := fmt.Sprintf("group_%d", e.GroupID)

	return platformEvent{
		Type:           EventTypeMessage,
		Kind:           event.KindGroupMessage,
		PlatformName:   platform,
		ChannelID:      channelID,
		ChannelName:    channelName,
		ChannelType:    "group",
		ChannelAvatar:  "",
		UserID:         senderID,
		UserName:       userName,
		UserAvatar:     "",
		MessageID:      messageID,
		SenderID:       senderID,
		SenderName:     userName,
		SenderNickname: e.Sender.Nickname,
		SenderAvatar:   "",
		ContentData: []platformContent{
			{Type: "text", Text: e.RawMessage},
		},
		ContentText: e.RawMessage,
		IsToMe:      false,
		Timestamp:   e.Time,
		IsSelf:      e.SelfID == e.UserID,
		RefChatKey:  "",
		RefMsgID:    "",
		RefSenderID: "",
	}
}

func buildBilibiliDMPlatformEvent(platform string, e bilibililive.LiveOpenPlatformDMEvent) platformEvent {
	userID := bilibiliUserID(e.OpenID, e.UID)
	result := buildBilibiliEventPlatformEvent(
		platform,
		event.KindDanmaku,
		e.RoomID,
		userID,
		e.UName,
		e.UFace,
		e.MessageID,
		e.Timestamp,
		e.Message,
	)
	result.RefSenderID = e.ReplyOpenID
	return result
}

func buildBilibiliGiftPlatformEvent(platform string, e bilibililive.LiveOpenPlatformSendGiftEvent) platformEvent {
	text := fmt.Sprintf("[礼物] 赠送 %s×%d（免费礼物）", e.GiftName, e.GiftNum)
	if e.Paid {
		value := float64(e.RPrice) * float64(e.GiftNum) / 1000
		text = fmt.Sprintf("[礼物] 赠送 %s×%d（价值 %s 元）", e.GiftName, e.GiftNum, formatBilibiliAmount(value))
	}

	return buildBilibiliEventPlatformEvent(
		platform,
		event.KindGift,
		e.RoomID,
		bilibiliUserID(e.OpenID, e.UID),
		e.UName,
		e.UFace,
		e.MessageID,
		e.Timestamp,
		text,
	)
}

func buildBilibiliSuperChatPlatformEvent(platform string, e bilibililive.LiveOpenPlatformSuperChatEvent) platformEvent {
	text := fmt.Sprintf("[醒目留言 %s元] %s", formatBilibiliAmount(e.RMB), e.Message)
	return buildBilibiliEventPlatformEvent(
		platform,
		event.KindSuperChat,
		e.RoomID,
		bilibiliUserID(e.OpenID, e.UID),
		e.UName,
		e.UFace,
		e.MsgID,
		e.Timestamp,
		text,
	)
}

func buildBilibiliGuardPlatformEvent(platform string, e bilibililive.LiveOpenPlatformGuardEvent) platformEvent {
	var openID, uname, uface string
	var uid int64
	if e.UserInfo != nil {
		uid = e.UserInfo.UID
		openID = e.UserInfo.OpenID
		uname = e.UserInfo.UName
		uface = e.UserInfo.UFace
	}

	text := fmt.Sprintf("[大航海] 开通%s×%d%s", bilibiliGuardName(e.GuardLevel), e.GuardNum, e.GuardUnit)
	return buildBilibiliEventPlatformEvent(
		platform,
		event.KindGuard,
		e.RoomID,
		bilibiliUserID(openID, uid),
		uname,
		uface,
		e.MessageID,
		e.Timestamp,
		text,
	)
}

func buildBilibiliNoticePlatformEvent(platform string, kind event.Kind, openID, uname, text string) platformEvent {
	return buildBilibiliEventPlatformEvent(
		platform,
		kind,
		0,
		openID,
		uname,
		"",
		"",
		0,
		text,
	)
}

// kindType 返回事件种类对应的粗粒度 type，保证新旧两个字段不会互相矛盾。
//
// 判据就是「是否需要回复」：需要回复的种类是 message，其余是 notice。
// 这与改动前的取值完全一致——点赞/进出直播间/开播下播一直是 notice，
// 下游据此丢弃它们，不要因为新增了 kind 字段而改变这一点。
func kindType(kind event.Kind) string {
	if kind.NeedsReply() {
		return EventTypeMessage
	}
	return EventTypeNotice
}

func buildBilibiliEventPlatformEvent(
	platform string,
	kind event.Kind,
	roomID int64,
	openID string,
	uname string,
	uface string,
	messageID string,
	timestamp int64,
	contentText string,
) platformEvent {
	userName := uname
	if userName == "" {
		userName = openID
	}

	channelID := ""
	channelName := ""
	channelType := ""
	if roomID != 0 {
		channelID = fmt.Sprintf("room_%d", roomID)
		channelName = fmt.Sprintf("直播间 %d", roomID)
		channelType = "live_room"
	}

	return platformEvent{
		Type:           kindType(kind),
		Kind:           kind,
		PlatformName:   platform,
		ChannelID:      channelID,
		ChannelName:    channelName,
		ChannelType:    channelType,
		ChannelAvatar:  "",
		UserID:         openID,
		UserName:       userName,
		UserAvatar:     uface,
		MessageID:      messageID,
		SenderID:       openID,
		SenderName:     userName,
		SenderNickname: uname,
		SenderAvatar:   uface,
		ContentData: []platformContent{
			{Type: "text", Text: contentText},
		},
		ContentText: contentText,
		IsToMe:      false,
		Timestamp:   timestamp,
		IsSelf:      false,
		RefChatKey:  "",
		RefMsgID:    "",
		RefSenderID: "",
	}
}

func bilibiliUserID(openID string, uid int64) string {
	if openID != "" {
		return openID
	}
	if uid != 0 {
		return strconv.FormatInt(uid, 10)
	}
	return ""
}

func bilibiliGuardName(level int64) string {
	switch level {
	case 1:
		return "总督"
	case 2:
		return "提督"
	case 3:
		return "舰长"
	default:
		return "大航海"
	}
}

func formatBilibiliAmount(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func buildNoticePlatformEvent(platform string, userID int64, text string) platformEvent {
	senderID := strconv.FormatInt(userID, 10)
	return platformEvent{
		Type:         EventTypeNotice,
		Kind:         event.KindNotice,
		PlatformName: platform,
		UserID:       senderID,
		UserName:     senderID,
		SenderID:     senderID,
		SenderName:   senderID,
		ContentData: []platformContent{
			{Type: "text", Text: text},
		},
		ContentText: text,
	}
}
