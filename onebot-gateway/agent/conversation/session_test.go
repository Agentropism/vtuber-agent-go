package conversation

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"onebot-gateway/agent/conversation/llm"
	"onebot-gateway/shared/action"
)

// messagePayload 构造上传管线投递的事件信封。
func messagePayload(t *testing.T, platform, channelID, channelType, text string) []byte {
	t.Helper()

	return buildPayload(t, map[string]any{
		"type":          "message",
		"platform_name": platform,
		"channel_id":    channelID,
		"channel_type":  channelType,
		"user_id":       "10001",
		"sender_name":   "小明",
		"content_text":  text,
		"message_id":    "msg-1",
		"is_self":       false,
	})
}

func buildPayload(t *testing.T, fields map[string]any) []byte {
	t.Helper()

	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("构造事件信封失败: %v", err)
	}
	return raw
}

// replyRecorder 记录下行 Action 并支持等待。
type replyRecorder struct {
	mu        sync.Mutex
	platforms []string
	replies   []action.Action
	ch        chan action.Action
}

func newReplyRecorder() *replyRecorder {
	return &replyRecorder{ch: make(chan action.Action, 8)}
}

func (r *replyRecorder) reply(platform string, act action.Action) error {
	r.mu.Lock()
	r.platforms = append(r.platforms, platform)
	r.replies = append(r.replies, act)
	r.mu.Unlock()

	r.ch <- act
	return nil
}

func (r *replyRecorder) wait(t *testing.T) action.Action {
	t.Helper()

	select {
	case act := <-r.ch:
		return act
	case <-time.After(2 * time.Second):
		t.Fatal("等待回复超时")
		return action.Action{}
	}
}

// blockingClient 是阻塞在流式调用里的假客户端，用于制造会话队列积压。
type blockingClient struct {
	release chan struct{}
}

func (c *blockingClient) ChatStream(ctx context.Context, req llm.Request, onEvent func(llm.Event) error) error {
	<-c.release
	return nil
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(message)
}

func TestParseInbound(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]any
		wantOK bool
	}{
		{
			name: "群消息",
			fields: map[string]any{
				"type": "message", "platform_name": "qq", "channel_id": "group_10001",
				"channel_type": "group", "content_text": "在吗", "sender_name": "小明",
			},
			wantOK: true,
		},
		{
			name: "通知事件被丢弃",
			fields: map[string]any{
				"type": "notice", "platform_name": "qq", "channel_id": "group_10001", "content_text": "有人入群",
			},
			wantOK: false,
		},
		{
			name: "机器人自身消息被丢弃",
			fields: map[string]any{
				"type": "message", "platform_name": "qq", "channel_id": "group_10001",
				"content_text": "我是机器人", "is_self": true,
			},
			wantOK: false,
		},
		{
			name: "空文本被丢弃",
			fields: map[string]any{
				"type": "message", "platform_name": "qq", "channel_id": "group_10001", "content_text": "   ",
			},
			wantOK: false,
		},
		{
			name: "缺少渠道标识被丢弃",
			fields: map[string]any{
				"type": "message", "platform_name": "qq", "content_text": "在吗",
			},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, ok := ParseInbound(buildPayload(t, tc.fields))
			if ok != tc.wantOK {
				t.Fatalf("判定结果不符: got=%v want=%v", ok, tc.wantOK)
			}
			if ok && event.Text != tc.fields["content_text"] {
				t.Errorf("文本不符合预期: %q", event.Text)
			}
		})
	}
}

func TestParseInboundFallbacksAndBadJSON(t *testing.T) {
	// 用户名为空时依次回退到 user_name、user_id
	event, ok := ParseInbound(buildPayload(t, map[string]any{
		"type": "message", "platform_name": "bilibili", "channel_id": "room_123",
		"channel_type": "live_room", "content_text": "主播好", "user_id": "open_abc",
	}))
	if !ok {
		t.Fatal("应判定为可处理事件")
	}
	if event.UserName != "open_abc" {
		t.Errorf("用户名回退失败: %q", event.UserName)
	}

	if _, ok := ParseInbound([]byte("不是 JSON")); ok {
		t.Error("非法 JSON 应判定为不可处理")
	}
}

func TestBuildReply(t *testing.T) {
	act, ok := buildReply(InboundEvent{
		Platform: PlatformQQ, ChannelID: "group_10001", ChannelType: channelTypeGroup,
	}, "你好")
	if !ok {
		t.Fatal("QQ 群消息应生成下行动作")
	}
	if act.Action != "send_group_msg" {
		t.Errorf("动作名不符合预期: %q", act.Action)
	}
	params, isParams := act.Params.(action.SendGroupMsgParams)
	if !isParams {
		t.Fatalf("参数类型不符合预期: %T", act.Params)
	}
	if params.GroupID != 10001 || params.Message != "你好" {
		t.Errorf("参数内容不符合预期: %+v", params)
	}

	cases := []struct {
		name  string
		event InboundEvent
	}{
		{"B站弹幕无下行动作", InboundEvent{Platform: PlatformBilibili, ChannelID: "room_123", ChannelType: channelTypeLiveRoom}},
		{"QQ 私聊无下行动作", InboundEvent{Platform: PlatformQQ, ChannelID: "user_1", ChannelType: "private"}},
		{"群号非法", InboundEvent{Platform: PlatformQQ, ChannelID: "group_abc", ChannelType: channelTypeGroup}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := buildReply(tc.event, "你好"); ok {
				t.Error("不应生成下行动作")
			}
		})
	}
}

func TestSessionsRepliesToQQGroup(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("你好呀")}}
	recorder := newReplyRecorder()

	sessions, err := NewSessions(SessionsConfig{LLM: client, Reply: recorder.reply})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "在吗"))

	act := recorder.wait(t)
	if act.Action != "send_group_msg" {
		t.Fatalf("动作名不符合预期: %q", act.Action)
	}
	params, isParams := act.Params.(action.SendGroupMsgParams)
	if !isParams {
		t.Fatalf("参数类型不符合预期: %T", act.Params)
	}
	if params.GroupID != 10001 {
		t.Errorf("群号不符合预期: %d", params.GroupID)
	}
	if params.Message != "你好呀" {
		t.Errorf("回复内容不符合预期: %q", params.Message)
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.platforms) != 1 || recorder.platforms[0] != PlatformQQ {
		t.Errorf("回复平台不符合预期: %v", recorder.platforms)
	}
}

func TestSessionsKeepsHistoryPerChannel(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("一"), textEvents("二")}}
	recorder := newReplyRecorder()

	sessions, err := NewSessions(SessionsConfig{LLM: client, Reply: recorder.reply})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "第一句"))
	recorder.wait(t)
	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "第二句"))
	recorder.wait(t)

	second := client.callAt(t, 1)
	var got []string
	for _, message := range second.Messages {
		got = append(got, message.Role+":"+message.Content)
	}
	want := []string{"user:第一句", "assistant:一", "user:第二句"}
	if len(got) != len(want) {
		t.Fatalf("同一渠道应复用会话历史: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("同一渠道历史不符合预期: %v", got)
		}
	}
}

func TestSessionsIsolateChannels(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("甲"), textEvents("乙")}}
	recorder := newReplyRecorder()

	sessions, err := NewSessions(SessionsConfig{LLM: client, Reply: recorder.reply})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "给甲"))
	recorder.wait(t)
	sessions.Handle(messagePayload(t, PlatformQQ, "group_20002", channelTypeGroup, "给乙"))
	recorder.wait(t)

	second := client.callAt(t, 1)
	if len(second.Messages) != 1 {
		t.Fatalf("不同渠道的历史必须互相隔离，实际 messages=%d 条", len(second.Messages))
	}
	if second.Messages[0].Content != "给乙" {
		t.Errorf("第二个渠道收到了串台内容: %q", second.Messages[0].Content)
	}
}

func TestSessionsSkipsBilibiliReply(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("你好")}}
	recorder := newReplyRecorder()

	sessions, err := NewSessions(SessionsConfig{LLM: client, Reply: recorder.reply})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformBilibili, "room_123", channelTypeLiveRoom, "主播好"))

	waitFor(t, func() bool { return client.callCount() > 0 }, "B站弹幕仍应进入会话生成")

	select {
	case act := <-recorder.ch:
		t.Fatalf("B站弹幕不应产生下行动作，实际: %+v", act)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSessionsDropsWhenQueueFull(t *testing.T) {
	client := &blockingClient{release: make(chan struct{})}
	sessions, err := NewSessions(SessionsConfig{LLM: client, QueueSize: 1})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(func() {
		close(client.release)
		sessions.Close()
	})

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "第一条"))
	waitFor(t, func() bool { return queueLen(sessions, "group_10001") == 0 }, "消费协程未取走首条消息")

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "第二条"))
	if got := queueLen(sessions, "group_10001"); got != 1 {
		t.Fatalf("第二条应占满队列，实际长度=%d", got)
	}

	// 队列已满：Handle 必须立即返回且不入队，不能阻塞上传管线的消费协程
	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "第三条"))
	if got := queueLen(sessions, "group_10001"); got != 1 {
		t.Fatalf("队列满时应丢弃，实际长度=%d", got)
	}
}

func TestSessionsHandleAfterClose(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("你好")}}
	sessions, err := NewSessions(SessionsConfig{LLM: client})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}

	sessions.Close()
	sessions.Close() // 重复关闭应幂等

	// 关闭后收到事件不应 panic
	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "在吗"))
}

func TestSessionsWithoutReplyFunc(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("你好")}}
	sessions, err := NewSessions(SessionsConfig{LLM: client})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	// 未注入 Reply 时只生成不发送，不应 panic
	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "在吗"))
	waitFor(t, func() bool { return client.callCount() > 0 }, "未注入 Reply 时仍应生成")
}

func TestNewSessionsValidation(t *testing.T) {
	if _, err := NewSessions(SessionsConfig{}); err == nil {
		t.Error("缺少 LLM 时应报错")
	}
}

// queueLen 读取指定渠道会话的待处理队列长度。
func queueLen(sessions *Sessions, channelID string) int {
	sessions.mu.Lock()
	current := sessions.byChannel[channelID]
	sessions.mu.Unlock()
	if current == nil {
		return 0
	}
	return len(current.in)
}
