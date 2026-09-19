package conversation

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/agent/conversation/llm"
	"github.com/Agentropism/vtuber-agent-go/internal/shared/event"
)

// fakeMemory 记录调用并按预设返回召回结果。
type fakeMemory struct {
	mu       sync.Mutex
	recalls  []string
	remember []MemoryEntry
	hits     []MemoryEntry
}

func (m *fakeMemory) Recall(query string, limit int) []MemoryEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.recalls = append(m.recalls, query)
	return m.hits
}

func (m *fakeMemory) Remember(entry MemoryEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.remember = append(m.remember, entry)
}

func (m *fakeMemory) snapshot() ([]string, []MemoryEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]string(nil), m.recalls...), append([]MemoryEntry(nil), m.remember...)
}

// 召回结果要出现在本轮的系统提示里，而不是被拼进用户消息（拼进去会污染历史）。
func TestSessionsInjectsRecallIntoSystemPrompt(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("记得呀")}}
	recorder := newReplyRecorder()
	memories := &fakeMemory{hits: []MemoryEntry{
		{User: "小明", Text: "我昨天说我喜欢拉面", Reply: "那我记住了"},
	}}

	sessions, err := NewSessions(SessionsConfig{LLM: client, Reply: recorder.reply, Memory: memories})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "我喜欢吃什么"))
	recorder.wait(t)

	recalls, _ := memories.snapshot()
	if len(recalls) != 1 || recalls[0] != "我喜欢吃什么" {
		t.Fatalf("召回查询不符合预期: %v", recalls)
	}

	request := client.callAt(t, 0)
	if !strings.Contains(request.System, "我昨天说我喜欢拉面") {
		t.Fatalf("系统提示里没有召回内容: %q", request.System)
	}
	for _, message := range request.Messages {
		if strings.Contains(message.Content, "我昨天说我喜欢拉面") {
			t.Fatalf("召回内容不该出现在对话消息里: %#v", message)
		}
	}
}

// 一轮结束后要把这轮记下来，权重按事件种类折算。
func TestSessionsRemembersTurnWithWeight(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("谢谢你的礼物")}}
	memories := &fakeMemory{}

	sessions, err := NewSessions(SessionsConfig{LLM: client, Memory: memories})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	payload := buildPayload(t, map[string]any{
		"type":          "message",
		"event_kind":    string(event.KindSuperChat),
		"platform_name": PlatformBilibili,
		"channel_id":    "room_123",
		"channel_type":  channelTypeLiveRoom,
		"user_id":       "open_1",
		"sender_name":   "老板",
		"content_text":  "加油！",
		"message_id":    "msg-sc-1",
	})
	sessions.Handle(payload)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, remembered := memories.snapshot(); len(remembered) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	_, remembered := memories.snapshot()
	if len(remembered) != 1 {
		t.Fatalf("记忆条数 = %d, want 1", len(remembered))
	}
	entry := remembered[0]
	if entry.ChannelID != "room_123" || entry.User != "老板" || entry.Text != "加油！" {
		t.Fatalf("记忆内容不符合预期: %#v", entry)
	}
	if entry.Reply != "谢谢你的礼物" {
		t.Fatalf("回复没有记进记忆: %q", entry.Reply)
	}
	if entry.Weight != 5 {
		t.Fatalf("醒目留言的权重 = %d, want 5", entry.Weight)
	}
}

func TestMemoryWeight(t *testing.T) {
	cases := map[event.Kind]int{
		event.KindSuperChat:     5,
		event.KindGift:          3,
		event.KindGuard:         3,
		event.KindDanmaku:       1,
		event.KindGroupMessage:  1,
		event.KindLiveRoomEnter: 1,
	}
	for kind, want := range cases {
		if got := memoryWeight(kind); got != want {
			t.Fatalf("%s 的权重 = %d, want %d", kind, got, want)
		}
	}
}

// 没配记忆时不应产生任何额外行为。
func TestSessionsWithoutMemory(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("好")}}

	sessions, err := NewSessions(SessionsConfig{LLM: client})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "在吗"))

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if client.callCount() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if client.callCount() == 0 {
		t.Fatal("没有记忆时也应该正常处理消息")
	}
}
