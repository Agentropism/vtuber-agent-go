package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"onebot-gateway/gateway/event/bilibililive"
	event "onebot-gateway/gateway/event/onebot"
	wordfilter "onebot-gateway/gateway/filter"
	"onebot-gateway/shared/action"

	"github.com/coder/websocket"
	"go.uber.org/zap"
)

var log = zap.NewNop()

// 事件类型常量
const (
	EventTypeMessage = "message"
	EventTypeNotice  = "notice"
)

type platformEvent struct {
	Type           string            `json:"type"`
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

// ---- 长连接管理 ----

var (
	wsConn           *websocket.Conn
	writeCh          chan []byte // 层1：有界缓存，Init 时按 QueueSize 创建
	writeMu          sync.Mutex  // 层2：远程写锁，同一时刻只允许一个在途上传
	done             = make(chan struct{})
	targetURL        string
	callbackPlatform string
	queueWaitTimeout time.Duration // 层1：缓存满时入队等待上限，0=无限等待
	noConsumer       bool          // 层2：目标未配置时无消费端，入队直接丢弃
	filter           *wordfilter.Filter

	// lifecycleMu 串行化 Init/Shutdown 与包级状态重写，防止与前一生命周期遗留的 connectLoop 协程竞争
	lifecycleMu sync.Mutex

	// forwardAction 远程 Action 转发回调，由 app 注入（server.SendAction），
	// 避免 upload 包导入 server 包造成循环依赖。
	forwardAction func(platform string, act action.Action) error
)

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

// SetActionForwarder 注入远程 Action 转发回调（server.SendAction），解除 server↔upload 循环依赖。
func SetActionForwarder(fn func(platform string, act action.Action) error) {
	forwardAction = fn
}

// Init 建立到记忆服务的 WebSocket 长连接，启动后台读写协程。
// 连接断开后自动重连。opts 为上传管线配置。
// 目标地址为空时仅记录错误并跳过初始化（缓存仍创建，避免 nil 通道阻塞入队）。
func Init(target string, callbackTargetPlatform string, opts Options) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()

	// 先关闭旧 done：保证前一生命周期遗留的 connectLoop 协程观察到关闭信号后退出，
	// 再重写包级状态，避免与读 `<-done`、`websocket.Dial(ctx, targetURL, ...)` 的旧协程竞争
	select {
	case <-done:
	default:
		close(done)
	}

	size := opts.QueueSize
	if size <= 0 {
		size = 256
	}
	writeCh = make(chan []byte, size)
	done = make(chan struct{})
	noConsumer = false

	if target == "" {
		noConsumer = true
		log.Sugar().Error("上传目标地址为空，跳过初始化")
		return nil
	}
	targetURL = target
	callbackPlatform = callbackTargetPlatform
	queueWaitTimeout = opts.QueueWaitTimeout

	f, err := wordfilter.New(opts.SensitiveWordsFile, opts.DedupTTL)
	if err != nil {
		return fmt.Errorf("创建敏感词过滤器: %w", err)
	}
	filter = f

	go connectLoop(targetURL, done)
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

// connectLoop 持锁建立连接并自动重连。
// url/stop 为 Init 持锁时传入的快照（go 语句参数求值建立 happens-before），
// 循环内只读快照，杜绝与后续 Init 在锁内重写 done/targetURL 竞争。
func connectLoop(url string, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			closeConn()
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
			CompressionMode: websocket.CompressionDisabled,
		})
		cancel()
		if err != nil {
			log.Sugar().Errorf("上传 WebSocket 连接失败: %v，正在重试...", err)
			sleepOrDone(stop, 3*time.Second)
			continue
		}

		conn.SetReadLimit(10 * 1024 * 1024)
		wsConn = conn

		log.Sugar().Info("上传 WebSocket 已连接")

		var wg sync.WaitGroup
		wg.Add(2)
		go readLoop(conn, &wg)
		go writeLoop(conn, &wg)
		wg.Wait()

		wsConn = nil

		select {
		case <-stop:
			return
		default:
			log.Sugar().Warn("上传 WebSocket 已断开，正在重连...")
			sleepOrDone(stop, 1*time.Second)
		}
	}
}

func sleepOrDone(stop <-chan struct{}, d time.Duration) {
	select {
	case <-stop:
	case <-time.After(d):
	}
}

func closeConn() {
	if wsConn != nil {
		wsConn.Close(websocket.StatusNormalClosure, "shutdown")
		wsConn = nil
	}
}

func readLoop(conn *websocket.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-done:
			return
		default:
		}
		_, payload, err := conn.Read(context.Background())
		if err != nil {
			log.Sugar().Debugf("上传 WebSocket 读取结束: %v", err)
			return
		}
		go handleCallbackAction(payload)

	}
}

// handleCallbackAction 处理远程写回的 Action。
func handleCallbackAction(payload []byte) {
	var act action.Action
	if err := json.Unmarshal(payload, &act); err != nil {
		log.Sugar().Errorf("解析回调事件失败: %v", err)
		return
	}
	log.Sugar().Infof("收到远程 Action: action=%s params=%v echo=%v target_platform=%s", act.Action, act.Params, act.Echo, callbackPlatform)
	if forwardAction == nil {
		log.Sugar().Error("未注入 Action 转发回调，忽略远程 Action")
		return
	}
	if err := forwardAction(callbackPlatform, act); err != nil {
		log.Sugar().Errorf("转发远程 Action 失败: %v", err)
		return
	}
	log.Sugar().Infof("远程 Action 已转发: action=%s target_platform=%s", act.Action, callbackPlatform)
}

func writeLoop(conn *websocket.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-done:
			return
		case payload := <-writeCh:
			// 层2：获取远程写锁，保证同一时刻只有一个在途上传
			writeMu.Lock()
			err := conn.Write(context.Background(), websocket.MessageText, payload)
			writeMu.Unlock()
			if err != nil {
				log.Sugar().Errorf("上传 WebSocket 写入失败: %v", err)
				return
			}
		}
	}
}

// ---- 公共入口 ----

// Upload 异步上传群消息事件。
func Upload(ctx context.Context, platform string, e event.MessageGroupEvent) {
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
func UploadBilibiliNotice(ctx context.Context, platform, openID, uname, text string) {
	upload(ctx, buildBilibiliNoticePlatformEvent(platform, openID, uname, text))
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
	// 目标未配置时无消费端：非阻塞丢弃，避免无限等待挂起事件链
	if noConsumer {
		log.Sugar().Warn("上传目标未配置，丢弃事件")
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
		case writeCh <- payload:
			log.Sugar().Debug("上传事件已入队")
		case <-done:
			log.Sugar().Warn("上传缓存已满且网关关闭，丢弃事件")
		}
		return
	}
	timer := time.NewTimer(queueWaitTimeout)
	defer timer.Stop()
	select {
	case writeCh <- payload:
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

func buildPlatformEvent(platform string, e event.MessageGroupEvent) platformEvent {
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
		EventTypeMessage,
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
		EventTypeMessage,
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
		EventTypeMessage,
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
		EventTypeMessage,
		e.RoomID,
		bilibiliUserID(openID, uid),
		uname,
		uface,
		e.MessageID,
		e.Timestamp,
		text,
	)
}

func buildBilibiliNoticePlatformEvent(platform, openID, uname, text string) platformEvent {
	return buildBilibiliEventPlatformEvent(
		platform,
		EventTypeNotice,
		0,
		openID,
		uname,
		"",
		"",
		0,
		text,
	)
}

func buildBilibiliEventPlatformEvent(
	platform string,
	eventType string,
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
		Type:           eventType,
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
