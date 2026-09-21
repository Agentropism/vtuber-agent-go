package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation/llm"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/action"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/emotion"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/event"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// SetLogger 之外：平台名与渠道类型取自上传事件信封的既有取值。
const (
	PlatformQQ       = "qq"
	PlatformBilibili = "bilibili"

	channelTypeGroup    = "group"
	channelTypeLiveRoom = "live_room"
)

// 会话默认参数。
const (
	defaultSessionQueue = 16
	defaultTurnTimeout  = 60 * time.Second
	channelPrefixGroup  = event.ChannelPrefixGroup
	eventTypeMessage    = "message"
)

// InboundEvent 是从平台事件适配出来的一条待处理消息，对应执行计划 E3 的「统一输入」。
type InboundEvent struct {
	Kind        event.Kind // 事件种类，决定播报优先级
	Platform    string     // qq / bilibili
	ChannelID   string     // group_10001 / room_12345
	ChannelType string     // group / live_room
	UserID      string
	UserName    string
	Text        string
	MessageID   string
}

// platformEventPayload 是上传管线投递的事件信封。
//
// 字段与 docs/EVENT_CONTRACT.md 的事件信封对齐；本包只解析会话所需的子集。
type platformEventPayload struct {
	Type         string     `json:"type"`
	Kind         event.Kind `json:"event_kind"`
	PlatformName string     `json:"platform_name"`
	ChannelID    string     `json:"channel_id"`
	ChannelType  string     `json:"channel_type"`
	UserID       string     `json:"user_id"`
	UserName     string     `json:"user_name"`
	SenderName   string     `json:"sender_name"`
	ContentText  string     `json:"content_text"`
	MessageID    string     `json:"message_id"`
	IsSelf       bool       `json:"is_self"`
}

// ParseInbound 解析事件信封，判断这条事件是否需要交给会话处理。
//
// 判据是事件种类（event_kind）是否需要回复：弹幕、礼物、醒目留言、大航海与
// QQ 群消息进会话，点赞、进出直播间、开播下播与各类通知只做记录。
// 兼容尚未写入 event_kind 的生产端：此时退回粗粒度的 type=="message" 判据，
// 并把种类按弹幕处理。机器人自身消息与空文本一律丢弃。
func ParseInbound(payload []byte) (InboundEvent, bool) {
	var envelope platformEventPayload
	if err := json.Unmarshal(payload, &envelope); err != nil {
		logger.Warnf("解析事件信封失败: %v", err)
		return InboundEvent{}, false
	}

	if envelope.IsSelf {
		return InboundEvent{}, false
	}

	kind := envelope.Kind
	if kind == "" {
		if envelope.Type != eventTypeMessage {
			return InboundEvent{}, false
		}
		kind = event.KindDanmaku
	}
	if !kind.NeedsReply() {
		return InboundEvent{}, false
	}

	text := strings.TrimSpace(envelope.ContentText)
	if text == "" || envelope.ChannelID == "" {
		return InboundEvent{}, false
	}

	name := envelope.SenderName
	if name == "" {
		name = envelope.UserName
	}
	if name == "" {
		name = envelope.UserID
	}

	return InboundEvent{
		Kind:        kind,
		Platform:    envelope.PlatformName,
		ChannelID:   envelope.ChannelID,
		ChannelType: envelope.ChannelType,
		UserID:      envelope.UserID,
		UserName:    name,
		Text:        text,
		MessageID:   envelope.MessageID,
	}, true
}

// ReplyFunc 把生成好的下行 Action 交回平台，由 app 注入 server.SendAction。
type ReplyFunc func(platform string, act action.Action) error

// Broadcaster 接收会话生成的回复，交给统一播报队列；broadcast.Queue 天然满足。
//
// 文本回复与语音播报是两条独立下行：文本回原平台（ReplyFunc），
// 语音进播报队列（本接口）。
type Broadcaster interface {
	Enqueue(item broadcast.Item) bool
}

// MemoryEntry 是一轮对话的记忆记录。
type MemoryEntry struct {
	Time      time.Time
	ChannelID string
	User      string
	Text      string
	Reply     string
	Weight    int
}

// Memory 是会话层需要的长期记忆能力（接口归调用方所有）。
type Memory interface {
	// Recall 按关键词召回历史，limit<=0 时由实现取默认值。
	Recall(query string, limit int) []MemoryEntry
	// Remember 记下一轮对话。
	Remember(entry MemoryEntry)
}

// SessionsConfig 是构造会话管理器的参数。
type SessionsConfig struct {
	LLM           ChatClient
	System        string
	Tools         []llm.Tool
	ToolExecutor  ToolExecutor
	InterruptMode InterruptMode
	MaxToolRounds int

	// 历史裁剪上限，<=0 取默认值（12 轮 / 4000 token）。
	MaxHistoryTurns  int
	MaxHistoryTokens int

	Memory      Memory // 长期记忆；为空时不做召回与记录
	RecallLimit int    // 每轮召回条数，<=0 取默认 5

	// IdleSpeakInterval > 0 时，渠道静默超过该时长就主动说一句（最低优先级，任何播报都能打断它）。
	IdleSpeakInterval time.Duration
	// IdleSpeakPrompt 是主动发言时给模型的提示词，为空用内置兜底。
	IdleSpeakPrompt string

	Reply     ReplyFunc   // 文本回复下行；为空时只生成不发送
	Broadcast Broadcaster // 语音播报队列；为空时跳过播报
	// Emotions 返回当前表情词表；非空时回复里的 [joy] 标签会被摘出来放进播报条目。
	// 用函数而不是快照：模型可以热切换，会话侧必须看到切换后的词表。
	Emotions    func() *emotion.Map
	QueueSize   int           // 每个会话的待处理消息上限，<=0 取默认 16
	TurnTimeout time.Duration // 单轮对话超时，<=0 取默认 60s
}

// Sessions 按 channel_id 维护一对一会话：同一渠道内串行、不同渠道并行。
//
// Handle 是 gateway/upload 的 Handler 实现；它只做解析与入队，立即返回，
// 真正的 LLM 调用在各自会话的协程中进行，绝不阻塞上传管线的消费协程。
type Sessions struct {
	cfg SessionsConfig

	mu        sync.Mutex
	byChannel map[string]*session
	closed    bool

	// idleDone 关闭时通知主动发言循环退出
	idleDone chan struct{}
	idleWg   sync.WaitGroup
}

// session 是单个渠道的会话：一条队列 + 一个消费协程，保证同渠道串行。
type session struct {
	channel string
	agent   *Agent
	in      chan InboundEvent
	done    chan struct{}
	wg      sync.WaitGroup
	cfg     *SessionsConfig

	// lastActive 是最近一次有真实事件的时间；lastIdle 是最近一次主动发言的时间。
	// 两者都用 Unix 纳秒存原子值，供主动发言循环跨协程读取。
	lastActive atomic.Int64
	lastIdle   atomic.Int64
}

var errSessionsClosed = errors.New("conversation: 会话管理器已关闭")

// NewSessions 构造会话管理器。
func NewSessions(cfg SessionsConfig) (*Sessions, error) {
	if cfg.LLM == nil {
		return nil, errors.New("conversation: LLM 不能为空")
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultSessionQueue
	}
	if cfg.TurnTimeout <= 0 {
		cfg.TurnTimeout = defaultTurnTimeout
	}

	sessions := &Sessions{
		cfg:       cfg,
		byChannel: make(map[string]*session),
		idleDone:  make(chan struct{}),
	}
	sessions.startIdleLoop()

	return sessions, nil
}

// Handle 解析一条上传事件并交给对应会话处理。
//
// 入队失败（会话队列已满）时丢弃并记录，避免拖慢上传管线。
func (s *Sessions) Handle(payload []byte) {
	event, ok := ParseInbound(payload)
	if !ok {
		return
	}

	current, err := s.sessionFor(event.ChannelID)
	if err != nil {
		logger.Warnf("会话不可用，丢弃事件: %v", err)
		return
	}

	select {
	case current.in <- event:
	default:
		logger.Warnf("会话 %s 队列已满，丢弃事件 message_id=%s", event.ChannelID, event.MessageID)
	}
}

// sessionFor 取指定渠道的会话，不存在则创建。
func (s *Sessions) sessionFor(channelID string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, errSessionsClosed
	}
	if current, ok := s.byChannel[channelID]; ok {
		return current, nil
	}

	agent, err := NewAgent(AgentConfig{
		LLM:              s.cfg.LLM,
		System:           s.cfg.System,
		Tools:            s.cfg.Tools,
		ToolExecutor:     s.cfg.ToolExecutor,
		InterruptMode:    s.cfg.InterruptMode,
		MaxToolRounds:    s.cfg.MaxToolRounds,
		MaxHistoryTurns:  s.cfg.MaxHistoryTurns,
		MaxHistoryTokens: s.cfg.MaxHistoryTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("创建渠道 %s 的会话: %w", channelID, err)
	}

	current := &session{
		channel: channelID,
		agent:   agent,
		in:      make(chan InboundEvent, s.cfg.QueueSize),
		done:    make(chan struct{}),
		cfg:     &s.cfg,
	}
	s.byChannel[channelID] = current

	current.wg.Add(1)
	go current.run()

	logger.Infof("已为渠道 %s 创建会话", channelID)
	return current, nil
}

// Close 停止所有会话并等待在途轮次结束。
func (s *Sessions) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.idleDone)
	sessions := make([]*session, 0, len(s.byChannel))
	for _, current := range s.byChannel {
		sessions = append(sessions, current)
	}
	s.mu.Unlock()

	for _, current := range sessions {
		close(current.done)
	}
	for _, current := range sessions {
		current.wg.Wait()
	}
	s.idleWg.Wait()

	logger.Info("会话管理器已关闭")
}

// run 串行消费本渠道的消息。
func (s *session) run() {
	defer s.wg.Done()

	for {
		select {
		case <-s.done:
			return
		case event := <-s.in:
			s.turn(event)
		}
	}
}

// turn 跑完一轮对话并把回复发回平台。
//
// 回复在流式生成过程中逐句入播报队列（不等整段完成），因此首字延迟取决于
// 第一句的生成与合成时间，而不是整段回复。文本下行仍用完整回复。
func (s *session) turn(event InboundEvent) {
	s.lastActive.Store(time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.TurnTimeout)
	defer cancel()

	var (
		reply    strings.Builder
		splitter = NewSplitter()
	)

	err := s.agent.ChatWithContext(ctx, event.Text, s.recallHint(event.Text), func(chunk string) error {
		reply.WriteString(chunk)
		for _, sentence := range splitter.Write(chunk) {
			s.enqueue(event, sentence)
		}
		return nil
	})
	if err != nil {
		// 出错时不发送半截文本回复；已经逐句入队的部分仍会播出去，
		// 这是流式投递的固有语义。
		logger.Errorf("渠道 %s 会话生成失败: %v", s.channel, err)
		return
	}

	for _, sentence := range splitter.Flush() {
		s.enqueue(event, sentence)
	}

	text := strings.TrimSpace(reply.String())
	if text == "" {
		logger.Warnf("渠道 %s 生成内容为空，不回复", s.channel)
		return
	}

	s.send(event, text)
	s.remember(event, text)
}

// recallHint 把召回的历史拼成一段系统提示；没有记忆或没有命中时返回空串。
func (s *session) recallHint(query string) string {
	if s.cfg.Memory == nil {
		return ""
	}

	hits := s.cfg.Memory.Recall(query, s.cfg.RecallLimit)
	if len(hits) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("以下是以前的对话片段，供参考，不要直接复述：")
	for _, hit := range hits {
		builder.WriteString("\n- ")
		if hit.User != "" {
			builder.WriteString(hit.User)
			builder.WriteString("：")
		}
		builder.WriteString(hit.Text)
		if hit.Reply != "" {
			builder.WriteString("（你当时回复：")
			builder.WriteString(hit.Reply)
			builder.WriteString("）")
		}
	}

	return builder.String()
}

// remember 记下这一轮；权重按事件种类折算，SC 与礼物更值得被想起来。
func (s *session) remember(event InboundEvent, reply string) {
	if s.cfg.Memory == nil {
		return
	}

	s.cfg.Memory.Remember(MemoryEntry{
		Time:      time.Now(),
		ChannelID: event.ChannelID,
		User:      event.UserName,
		Text:      event.Text,
		Reply:     reply,
		Weight:    memoryWeight(event.Kind),
	})
}

// memoryWeight 把事件种类折算成记忆权重。
func memoryWeight(kind event.Kind) int {
	switch kind {
	case event.KindSuperChat:
		return 5
	case event.KindGift, event.KindGuard:
		return 3
	default:
		return 1
	}
}

// enqueue 把一句待播报文本交给统一播报队列；未注入队列时跳过，只做文本下行。
//
// 优先级取自事件种类：SC > 礼物/大航海 > 弹幕与群消息。
//
// 同时处理表情标签：模型输出里的 [joy] 这类标签在这里被摘下来写进 Item.Emotion，
// 文本本身去掉标签后再播报——否则标签会被 TTS 逐字念出来。
func (s *session) enqueue(event InboundEvent, text string) {
	if s.cfg.Broadcast == nil {
		return
	}

	emotionName := ""
	if s.cfg.Emotions != nil {
		if labels := s.cfg.Emotions(); labels != nil {
			if names := labels.ExtractNames(text); len(names) > 0 {
				emotionName = names[0]
			}
			text = labels.Strip(text)
		}
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	item := broadcast.Item{
		Priority: broadcast.PriorityFor(event.Kind),
		Text:     text,
		Emotion:  emotionName,
		Source:   event.Platform + ":" + event.ChannelID,
	}
	if !s.cfg.Broadcast.Enqueue(item) {
		logger.Warnf("播报队列拒绝入队: source=%s priority=%s", item.Source, item.Priority)
	}
}

// send 把回复文本转成平台下行 Action 并交回平台。
func (s *session) send(event InboundEvent, text string) {
	act, ok := buildReply(event, text)
	if !ok {
		// 平台没有下行动作是常态（B 站弹幕没有发送接口），只播报不发送；
		// 这里用 Debug，免得每句回复都刷一条像是出错的告警。
		logger.Debugf("渠道 %s 所在平台没有下行动作，回复只播报不发送", s.channel)
		return
	}
	if s.cfg.Reply == nil {
		logger.Warn("未注入回复下行函数，回复未发送")
		return
	}
	if err := s.cfg.Reply(event.Platform, act); err != nil {
		logger.Errorf("发送回复失败: %v", err)
		return
	}
	logger.Infof("已回复渠道 %s: %s", s.channel, text)
}

// buildReply 把回复文本转成平台下行 Action。
//
// 目前只有 QQ 群有对应的下行动作（send_group_msg）；B 站弹幕没有可用的
// 发送接口，与原先 Python 实现 build_reply_action 的取舍一致。
func buildReply(event InboundEvent, text string) (action.Action, bool) {
	if event.Platform != PlatformQQ || event.ChannelType != channelTypeGroup {
		return action.Action{}, false
	}

	groupID, err := strconv.ParseInt(strings.TrimPrefix(event.ChannelID, channelPrefixGroup), 10, 64)
	if err != nil {
		return action.Action{}, false
	}

	return action.SendGroupMsg(action.SendGroupMsgParams{
		GroupID: groupID,
		Message: text,
	}), true
}

// ChannelStatus 是单个渠道会话的对外快照，供观测接口展示。
type ChannelStatus struct {
	ChannelID  string    `json:"channel_id"`
	Pending    int       `json:"pending"`     // 待处理事件数（会话队列长度）
	HistoryLen int       `json:"history_len"` // 历史消息条数
	LastActive time.Time `json:"last_active,omitempty"`
	LastIdle   time.Time `json:"last_idle,omitempty"`
}

// Channels 返回当前所有渠道会话的快照，按渠道号排序。
func (s *Sessions) Channels() []ChannelStatus {
	s.mu.Lock()
	current := make([]*session, 0, len(s.byChannel))
	for _, item := range s.byChannel {
		current = append(current, item)
	}
	s.mu.Unlock()

	statuses := make([]ChannelStatus, 0, len(current))
	for _, item := range current {
		status := ChannelStatus{
			ChannelID:  item.channel,
			Pending:    len(item.in),
			HistoryLen: len(item.agent.History()),
		}
		if at := item.lastActive.Load(); at > 0 {
			status.LastActive = time.Unix(0, at)
		}
		if at := item.lastIdle.Load(); at > 0 {
			status.LastIdle = time.Unix(0, at)
		}
		statuses = append(statuses, status)
	}

	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ChannelID < statuses[j].ChannelID })

	return statuses
}

// History 返回指定渠道的历史消息；该渠道还没有会话时返回 false。
//
// 只回可展示的对话：系统提示词含人设细节，不从这里出去。
func (s *Sessions) History(channelID string) ([]llm.Message, bool) {
	s.mu.Lock()
	item, ok := s.byChannel[channelID]
	s.mu.Unlock()
	if !ok {
		return nil, false
	}

	return item.agent.History(), true
}
