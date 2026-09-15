package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"onebot-gateway/agent/conversation/llm"
	"onebot-gateway/shared/action"

	"go.uber.org/zap"
)

var log = zap.NewNop()

// SetLogger 注入日志器，由 app.Initialize() 调用。
func SetLogger(l *zap.Logger) {
	if l != nil {
		log = l
	}
}

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
	channelPrefixGroup  = "group_"
	eventTypeMessage    = "message"
)

// InboundEvent 是从平台事件适配出来的一条待处理消息，对应执行计划 E3 的「统一输入」。
type InboundEvent struct {
	Platform    string // qq / bilibili
	ChannelID   string // group_10001 / room_12345
	ChannelType string // group / live_room
	UserID      string
	UserName    string
	Text        string
	MessageID   string
}

// platformEventPayload 是上传管线投递的事件信封。
//
// 字段与 docs/UPLOAD_API.md 的上行结构对齐；本包只解析会话所需的子集。
type platformEventPayload struct {
	Type         string `json:"type"`
	PlatformName string `json:"platform_name"`
	ChannelID    string `json:"channel_id"`
	ChannelType  string `json:"channel_type"`
	UserID       string `json:"user_id"`
	UserName     string `json:"user_name"`
	SenderName   string `json:"sender_name"`
	ContentText  string `json:"content_text"`
	MessageID    string `json:"message_id"`
	IsSelf       bool   `json:"is_self"`
}

// ParseInbound 解析事件信封，判断这条事件是否需要交给会话处理。
//
// 取舍与原先的 Python 记忆服务一致：只处理 message 类事件，
// 丢弃机器人自身消息、notice 通知与空文本。
func ParseInbound(payload []byte) (InboundEvent, bool) {
	var envelope platformEventPayload
	if err := json.Unmarshal(payload, &envelope); err != nil {
		log.Sugar().Warnf("解析事件信封失败: %v", err)
		return InboundEvent{}, false
	}

	if envelope.Type != eventTypeMessage || envelope.IsSelf {
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

// SessionsConfig 是构造会话管理器的参数。
type SessionsConfig struct {
	LLM           ChatClient
	System        string
	Tools         []llm.Tool
	ToolExecutor  ToolExecutor
	InterruptMode InterruptMode
	MaxToolRounds int

	Reply       ReplyFunc     // 回复下行；为空时只生成不发送
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
}

// session 是单个渠道的会话：一条队列 + 一个消费协程，保证同渠道串行。
type session struct {
	channel string
	agent   *Agent
	in      chan InboundEvent
	done    chan struct{}
	wg      sync.WaitGroup
	cfg     *SessionsConfig
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

	return &Sessions{
		cfg:       cfg,
		byChannel: make(map[string]*session),
	}, nil
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
		log.Sugar().Warnf("会话不可用，丢弃事件: %v", err)
		return
	}

	select {
	case current.in <- event:
	default:
		log.Sugar().Warnf("会话 %s 队列已满，丢弃事件 message_id=%s", event.ChannelID, event.MessageID)
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
		LLM:           s.cfg.LLM,
		System:        s.cfg.System,
		Tools:         s.cfg.Tools,
		ToolExecutor:  s.cfg.ToolExecutor,
		InterruptMode: s.cfg.InterruptMode,
		MaxToolRounds: s.cfg.MaxToolRounds,
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

	log.Sugar().Infof("已为渠道 %s 创建会话", channelID)
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

	log.Sugar().Info("会话管理器已关闭")
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
func (s *session) turn(event InboundEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.TurnTimeout)
	defer cancel()

	var reply strings.Builder
	err := s.agent.Chat(ctx, event.Text, func(chunk string) error {
		reply.WriteString(chunk)
		return nil
	})
	if err != nil {
		// 出错时不发送半截回复，避免把失败的生成内容播出去
		log.Sugar().Errorf("渠道 %s 会话生成失败: %v", s.channel, err)
		return
	}

	text := strings.TrimSpace(reply.String())
	if text == "" {
		log.Sugar().Warnf("渠道 %s 生成内容为空，不回复", s.channel)
		return
	}

	s.send(event, text)
}

// send 把回复文本转成平台下行 Action 并交回平台。
func (s *session) send(event InboundEvent, text string) {
	act, ok := buildReply(event, text)
	if !ok {
		log.Sugar().Warnf("渠道 %s 无对应的下行动作，回复未发送: %s", s.channel, text)
		return
	}
	if s.cfg.Reply == nil {
		log.Sugar().Warn("未注入回复下行函数，回复未发送")
		return
	}
	if err := s.cfg.Reply(event.Platform, act); err != nil {
		log.Sugar().Errorf("发送回复失败: %v", err)
		return
	}
	log.Sugar().Infof("已回复渠道 %s: %s", s.channel, text)
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
