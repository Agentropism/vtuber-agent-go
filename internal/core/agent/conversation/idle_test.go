package conversation

import (
	"sync"
	"testing"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation/llm"
)

// broadcastRecorder 记录入队的播报条目。
type broadcastRecorder struct {
	mu    sync.Mutex
	items []broadcast.Item
	ch    chan broadcast.Item
}

func newBroadcastRecorder() *broadcastRecorder {
	return &broadcastRecorder{ch: make(chan broadcast.Item, 8)}
}

func (r *broadcastRecorder) Enqueue(item broadcast.Item) bool {
	r.mu.Lock()
	r.items = append(r.items, item)
	r.mu.Unlock()

	select {
	case r.ch <- item:
	default:
	}
	return true
}

func (r *broadcastRecorder) wait(t *testing.T) broadcast.Item {
	t.Helper()

	select {
	case item := <-r.ch:
		return item
	case <-time.After(3 * time.Second):
		t.Fatal("等待播报入队超时")
		return broadcast.Item{}
	}
}

func (r *broadcastRecorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.items)
}

// 静默够久就主动说一句，且用最低优先级——任何弹幕都能打断它。
func TestIdleSpeakEnqueuesAtLowestPriority(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{
		textEvents("好安静呀"),   // 第一条真实回复
		textEvents("大家还在吗？"), // 主动发言
	}}
	recorder := newBroadcastRecorder()

	sessions, err := NewSessions(SessionsConfig{
		LLM:               client,
		Broadcast:         recorder,
		IdleSpeakInterval: 80 * time.Millisecond,
		TurnTimeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformBilibili, "room_1", channelTypeLiveRoom, "在吗"))

	first := recorder.wait(t)
	if first.Priority != broadcast.PriorityDanmaku {
		t.Fatalf("第一条优先级 = %s, want danmaku", first.Priority)
	}

	idle := recorder.wait(t)
	if idle.Priority != broadcast.PriorityIdle {
		t.Fatalf("主动发言优先级 = %s, want idle（最低，可被任何播报打断）", idle.Priority)
	}
	if idle.Text != "大家还在吗？" {
		t.Fatalf("主动发言内容 = %q", idle.Text)
	}
}

// 只在真的事件结束后才可能主动发言：一直有弹幕就不该插话。
func TestIdleSpeakWaitsForSilence(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("一"), textEvents("二"), textEvents("三")}}
	recorder := newBroadcastRecorder()

	sessions, err := NewSessions(SessionsConfig{
		LLM:               client,
		Broadcast:         recorder,
		IdleSpeakInterval: 500 * time.Millisecond,
		TurnTimeout:       2 * time.Second,
	})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformBilibili, "room_1", channelTypeLiveRoom, "一"))
	recorder.wait(t)
	sessions.Handle(messagePayload(t, PlatformBilibili, "room_1", channelTypeLiveRoom, "二"))
	recorder.wait(t)

	// 检查周期是 interval/4 = 125ms，300ms 内不该触发主动发言
	time.Sleep(300 * time.Millisecond)
	if got := recorder.len(); got != 2 {
		t.Fatalf("弹幕不断时不该主动发言，已有 %d 条播报", got)
	}
}

// 没开主动发言时什么都不会发生。
func TestIdleSpeakDisabledByDefault(t *testing.T) {
	client := &scriptedClient{turns: [][]llm.Event{textEvents("好")}}
	recorder := newBroadcastRecorder()

	sessions, err := NewSessions(SessionsConfig{LLM: client, Broadcast: recorder})
	if err != nil {
		t.Fatalf("创建会话管理器失败: %v", err)
	}
	t.Cleanup(sessions.Close)

	sessions.Handle(messagePayload(t, PlatformBilibili, "room_1", channelTypeLiveRoom, "在吗"))
	recorder.wait(t)

	time.Sleep(200 * time.Millisecond)
	if got := recorder.len(); got != 1 {
		t.Fatalf("未启用主动发言时不该有额外播报，已有 %d 条", got)
	}
}
