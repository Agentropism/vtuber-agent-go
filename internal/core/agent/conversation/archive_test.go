package conversation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation/llm"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/event"
)

// fakeArchive 记录被归档的对话，并按预设返回恢复用的历史。
type fakeArchive struct {
	mu        sync.Mutex
	dialogues []ArchiveRecord
	history   []ArchiveRecord
}

func (a *fakeArchive) RecordDialogue(record ArchiveRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.dialogues = append(a.dialogues, record)
}

func (a *fakeArchive) RecentDialogues(channelID string, limit int) []ArchiveRecord {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]ArchiveRecord(nil), a.history...)
}

func (a *fakeArchive) snapshot() []ArchiveRecord {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]ArchiveRecord(nil), a.dialogues...)
}

// 一轮对话结束后要连同回复写进归档，字段要能还原「谁在哪个渠道说了什么」。
func TestSessionsArchivesDialogue(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("你好呀")}}
	recorder := newReplyRecorder()
	arch := &fakeArchive{}

	sessions, err := NewSessions(SessionsConfig{LLM: client, Reply: recorder.reply, Archive: arch})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	payload := buildPayload(t, map[string]any{
		"type":          "message",
		"event_kind":    string(event.KindGroupMessage),
		"platform_name": PlatformQQ,
		"channel_id":    "group_10001",
		"channel_type":  channelTypeGroup,
		"user_id":       "10001",
		"sender_name":   "小明",
		"content_text":  "在吗",
		"is_self":       false,
	})
	sessions.Handle(payload)
	recorder.wait(t)

	waitFor(t, func() bool { return len(arch.snapshot()) == 1 }, "对话未写入归档")

	got := arch.snapshot()[0]
	if got.ChannelID != "group_10001" || got.Platform != PlatformQQ {
		t.Fatalf("归档渠道/平台不符合预期: %+v", got)
	}
	if got.UserID != "10001" || got.UserName != "小明" {
		t.Fatalf("归档用户不符合预期: %+v", got)
	}
	if got.EventKind != event.KindGroupMessage || got.Text != "在吗" || got.Reply != "你好呀" {
		t.Fatalf("归档内容不符合预期: %+v", got)
	}
}

// 渠道首次创建时要用归档恢复上下文：恢复的消息与正常轮次的历史同构，且不触发回复。
func TestSessionsRehydratesHistoryFromArchive(t *testing.T) {
	base := time.Date(2026, 9, 21, 22, 0, 0, 0, time.Local)
	arch := &fakeArchive{history: []ArchiveRecord{
		{Time: base, ChannelID: "group_10001", Platform: PlatformQQ, EventKind: event.KindGroupMessage, Text: "昨天说了什么", Reply: "说要去钓鱼"},
		{Time: base.Add(time.Minute), ChannelID: "group_10001", Platform: PlatformQQ, EventKind: event.KindGroupMessage, Text: "今天呢", Reply: "今天写代码"},
	}}

	client := &scriptedClient{turns: [][]llm.Event{textEvents("接上了")}}
	recorder := newReplyRecorder()

	sessions, err := NewSessions(SessionsConfig{
		LLM:            client,
		Reply:          recorder.reply,
		Archive:        arch,
		RehydrateTurns: 2,
	})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "还记得吗"))
	recorder.wait(t)

	request := client.callAt(t, 0)
	got := make([]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		got = append(got, message.Role+":"+message.Content)
	}
	want := []string{
		"user:昨天说了什么", "assistant:说要去钓鱼",
		"user:今天呢", "assistant:今天写代码",
		"user:还记得吗",
	}
	if len(got) != len(want) {
		t.Fatalf("恢复的历史不符合预期:\n实际: %v\n期望: %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("恢复的历史不符合预期:\n实际: %v\n期望: %v", got, want)
		}
	}
}

// RehydrateTurns=0 时不恢复：首轮请求只带本轮用户消息。
func TestSessionsRehydrateDisabled(t *testing.T) {
	arch := &fakeArchive{history: []ArchiveRecord{
		{ChannelID: "group_10001", Text: "旧消息", Reply: "旧回复"},
	}}

	client := &scriptedClient{turns: [][]llm.Event{textEvents("新回复")}}
	recorder := newReplyRecorder()

	sessions, err := NewSessions(SessionsConfig{LLM: client, Reply: recorder.reply, Archive: arch})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "新消息"))
	recorder.wait(t)

	request := client.callAt(t, 0)
	if len(request.Messages) != 1 || request.Messages[0].Content != "新消息" {
		t.Fatalf("关闭恢复后不该带旧历史: %+v", request.Messages)
	}
}

// 归档里没有回复的轮次只恢复用户消息，不造空助手消息。
func TestSessionsRehydrateSkipsEmptyReply(t *testing.T) {
	arch := &fakeArchive{history: []ArchiveRecord{
		{ChannelID: "group_10001", Text: "没有回复的旧消息", Reply: ""},
	}}

	client := &scriptedClient{turns: [][]llm.Event{textEvents("好")}}
	recorder := newReplyRecorder()

	sessions, err := NewSessions(SessionsConfig{
		LLM:            client,
		Reply:          recorder.reply,
		Archive:        arch,
		RehydrateTurns: 5,
	})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformQQ, "group_10001", channelTypeGroup, "新消息"))
	recorder.wait(t)

	request := client.callAt(t, 0)
	got := make([]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		got = append(got, message.Role+":"+message.Content)
	}
	want := []string{"user:没有回复的旧消息", "user:新消息"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("空回复轮次恢复不符合预期: %v", got)
	}
}

// 预置历史（InitialHistory）会被写进请求并按裁剪规则截断。
func TestAgentSeedsInitialHistory(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("好")}}
	agent, err := NewAgent(AgentConfig{
		LLM: client,
		InitialHistory: []llm.Message{
			{Role: llm.RoleUser, Content: "旧问题"},
			{Role: llm.RoleAssistant, Content: "旧回答"},
		},
	})
	if err != nil {
		t.Fatalf("构造 Agent 失败: %v", err)
	}

	if err := agent.Chat(context.Background(), "新问题", func(string) error { return nil }); err != nil {
		t.Fatalf("对话失败: %v", err)
	}

	request := client.callAt(t, 0)
	got := make([]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		got = append(got, message.Role+":"+message.Content)
	}
	want := []string{"user:旧问题", "assistant:旧回答", "user:新问题"}
	if len(got) != len(want) {
		t.Fatalf("预置历史不符合预期: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("预置历史不符合预期: %v", got)
		}
	}
}
