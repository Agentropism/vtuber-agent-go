package broadcast

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSynth 是可控的假合成器。
type fakeSynth struct {
	mu      sync.Mutex
	counts  map[string]int
	delay   time.Duration
	failing map[string]bool
}

func (s *fakeSynth) Synthesize(ctx context.Context, text string) ([]byte, error) {
	s.mu.Lock()
	if s.counts == nil {
		s.counts = make(map[string]int)
	}
	s.counts[text]++
	delay := s.delay
	fail := s.failing[text]
	s.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if fail {
		return nil, errors.New("合成失败: " + text)
	}
	return []byte(text), nil
}

func (s *fakeSynth) count(text string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[text]
}

// fakeSink 记录播报顺序，并支持让指定文本的播报一直占用到被释放或被打断。
type fakeSink struct {
	mu        sync.Mutex
	completed []Item
	started   []Item
	startedAt []time.Time
	current   string
	block     map[string]chan struct{}
}

func newFakeSink() *fakeSink {
	return &fakeSink{block: make(map[string]chan struct{})}
}

func (s *fakeSink) Play(ctx context.Context, item Item, pcm []byte) error {
	s.mu.Lock()
	s.started = append(s.started, item)
	s.startedAt = append(s.startedAt, time.Now())
	s.current = item.Text
	gate := s.block[item.Text]
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if s.current == item.Text {
			s.current = ""
		}
		s.mu.Unlock()
	}()

	if gate == nil {
		s.markCompleted(item)
		return nil
	}

	select {
	case <-gate:
		s.markCompleted(item)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *fakeSink) markCompleted(item Item) {
	s.mu.Lock()
	s.completed = append(s.completed, item)
	s.mu.Unlock()
}

func (s *fakeSink) snapshot() (completed, started []Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Item(nil), s.completed...), append([]Item(nil), s.started...)
}

func (s *fakeSink) completedTexts() []string {
	completed, _ := s.snapshot()
	return texts(completed)
}

func (s *fakeSink) startedTexts() []string {
	_, started := s.snapshot()
	return texts(started)
}

func (s *fakeSink) currentText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func texts(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Text)
	}
	return out
}

func newTestQueue(t *testing.T, cfg Config) *Queue {
	t.Helper()

	queue, err := New(cfg)
	if err != nil {
		t.Fatalf("构造播报队列失败: %v", err)
	}
	t.Cleanup(queue.Close)

	return queue
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", message)
}

func assertOrder(t *testing.T, got []string, want ...string) {
	t.Helper()

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("播报顺序不符合预期:\n实际: %v\n期望: %v", got, want)
	}
}

func TestSamePriorityIsFIFO(t *testing.T) {
	sink := newFakeSink()
	queue := newTestQueue(t, Config{Synth: &fakeSynth{}, Sink: sink})

	for _, text := range []string{"一", "二", "三"} {
		queue.Enqueue(Item{Priority: PriorityDanmaku, Text: text})
	}

	waitFor(t, func() bool { return len(sink.completedTexts()) == 3 }, "三段弹幕未播完")
	assertOrder(t, sink.completedTexts(), "一", "二", "三")
}

// TestDeliveryFollowsPriority 覆盖「优先级分层」：先入队的低优先级不会抢在
// 后入队的高优先级前面播出去。
func TestDeliveryFollowsPriority(t *testing.T) {
	sink := newFakeSink()
	queue := newTestQueue(t, Config{Synth: &fakeSynth{}, Sink: sink})

	queue.Enqueue(Item{Priority: PriorityIdle, Text: "待机"})
	queue.Enqueue(Item{Priority: PriorityDanmaku, Text: "弹幕"})
	queue.Enqueue(Item{Priority: PrioritySuperChat, Text: "醒目留言"})

	waitFor(t, func() bool { return len(sink.completedTexts()) == 3 }, "三段未播完")
	assertOrder(t, sink.completedTexts(), "醒目留言", "弹幕", "待机")
}

// TestSuperChatPreemptsDanmaku 是执行计划里的验收场景。
func TestSuperChatPreemptsDanmaku(t *testing.T) {
	synth := &fakeSynth{}
	sink := newFakeSink()
	gate := make(chan struct{})
	sink.block["弹幕回复"] = gate

	queue := newTestQueue(t, Config{Synth: synth, Sink: sink})

	queue.Enqueue(Item{Priority: PriorityDanmaku, Text: "弹幕回复"})
	waitFor(t, func() bool { return sink.currentText() == "弹幕回复" }, "弹幕未开始播报")

	queue.Enqueue(Item{Priority: PrioritySuperChat, Text: "醒目留言"})
	// 断言「已开始」而不是「正在播报」：醒目留言可能瞬间就播完，轮询会错过中间态
	waitFor(t, func() bool { return len(sink.startedTexts()) >= 2 }, "醒目留言未抢占")

	if got := sink.startedTexts(); got[1] != "醒目留言" {
		t.Fatalf("抢占顺序不符合预期: %v", got)
	}

	// 放开闸门，让被打断后重新排队的弹幕能播完
	close(gate)
	waitFor(t, func() bool { return len(sink.completedTexts()) == 2 }, "两段未播完")

	assertOrder(t, sink.startedTexts(), "弹幕回复", "醒目留言", "弹幕回复")
	assertOrder(t, sink.completedTexts(), "醒目留言", "弹幕回复")

	// 被打断的条目复用已合成的音频，不应重新调用合成
	if got := synth.count("弹幕回复"); got != 1 {
		t.Errorf("被打断的条目不应重新合成，实际合成 %d 次", got)
	}
	if stats := queue.Stats(); stats.Preempted != 1 {
		t.Errorf("抢占计数不符合预期: %d", stats.Preempted)
	}
}

// TestIdleCanBePreemptedByAnyPriority 覆盖「待机发言最低级可随时打断」。
func TestIdleCanBePreemptedByAnyPriority(t *testing.T) {
	sink := newFakeSink()
	gate := make(chan struct{})
	sink.block["待机发言"] = gate

	queue := newTestQueue(t, Config{Synth: &fakeSynth{}, Sink: sink})

	queue.Enqueue(Item{Priority: PriorityIdle, Text: "待机发言"})
	waitFor(t, func() bool { return sink.currentText() == "待机发言" }, "待机发言未开始")

	queue.Enqueue(Item{Priority: PriorityProactive, Text: "主动互动"})
	waitFor(t, func() bool { return len(sink.startedTexts()) >= 2 }, "待机发言未被抢占")

	if got := sink.startedTexts(); got[1] != "主动互动" {
		t.Fatalf("抢占顺序不符合预期: %v", got)
	}

	close(gate)
	waitFor(t, func() bool { return len(sink.completedTexts()) == 2 }, "两段未播完")

	if stats := queue.Stats(); stats.Preempted != 1 {
		t.Errorf("抢占计数不符合预期: %d", stats.Preempted)
	}
}

func TestLowerPriorityDoesNotPreemptHigher(t *testing.T) {
	sink := newFakeSink()
	gate := make(chan struct{})
	sink.block["醒目留言"] = gate

	queue := newTestQueue(t, Config{Synth: &fakeSynth{}, Sink: sink})

	queue.Enqueue(Item{Priority: PrioritySuperChat, Text: "醒目留言"})
	waitFor(t, func() bool { return sink.currentText() == "醒目留言" }, "醒目留言未开始")

	queue.Enqueue(Item{Priority: PriorityIdle, Text: "待机发言"})
	time.Sleep(50 * time.Millisecond)

	if sink.currentText() != "醒目留言" {
		t.Fatalf("低优先级不应打断高优先级，当前播报: %q", sink.currentText())
	}

	close(gate)
	waitFor(t, func() bool { return len(sink.completedTexts()) == 2 }, "两段未播完")
	assertOrder(t, sink.completedTexts(), "醒目留言", "待机发言")
}

func TestCooldownSpacesBroadcasts(t *testing.T) {
	sink := newFakeSink()
	const cooldown = 150 * time.Millisecond

	queue := newTestQueue(t, Config{
		Synth:       &fakeSynth{},
		Sink:        sink,
		MinInterval: cooldown,
	})

	queue.Enqueue(Item{Priority: PriorityDanmaku, Text: "一"})
	queue.Enqueue(Item{Priority: PriorityDanmaku, Text: "二"})
	waitFor(t, func() bool { return len(sink.completedTexts()) == 2 }, "两段未播完")

	sink.mu.Lock()
	first, second := sink.startedAt[0], sink.startedAt[1]
	sink.mu.Unlock()

	if gap := second.Sub(first); gap < cooldown/2 {
		t.Errorf("冷却未生效，两段间隔仅 %v", gap)
	}
}

func TestQueueCapacityEvictsLowestPriority(t *testing.T) {
	synth := &fakeSynth{delay: 300 * time.Millisecond}
	sink := newFakeSink()

	queue := newTestQueue(t, Config{
		Synth:       synth,
		Sink:        sink,
		MaxPending:  2,
		Concurrency: 1,
	})

	queue.Enqueue(Item{Priority: PriorityIdle, Text: "待机"})
	queue.Enqueue(Item{Priority: PriorityDanmaku, Text: "弹幕"})
	// 队列已满：本条优先级最高，应淘汰「待机」
	if ok := queue.Enqueue(Item{Priority: PrioritySuperChat, Text: "醒目留言"}); !ok {
		t.Fatal("高优先级条目应被接受")
	}

	waitFor(t, func() bool { return len(sink.completedTexts()) == 2 }, "两段未播完")

	if got := synth.count("待机"); got != 0 {
		t.Errorf("最低优先级条目应被淘汰，实际合成 %d 次", got)
	}
	if stats := queue.Stats(); stats.Dropped != 1 {
		t.Errorf("丢弃计数不符合预期: %d", stats.Dropped)
	}
}

func TestQueueRejectsLowestPriorityWhenFull(t *testing.T) {
	synth := &fakeSynth{delay: 300 * time.Millisecond}
	queue := newTestQueue(t, Config{
		Synth:       synth,
		Sink:        newFakeSink(),
		MaxPending:  1,
		Concurrency: 1,
	})

	queue.Enqueue(Item{Priority: PrioritySuperChat, Text: "醒目留言"})
	// 队列已满且本条优先级更低：应被丢弃，而不是挤掉高优先级
	if ok := queue.Enqueue(Item{Priority: PriorityIdle, Text: "待机"}); ok {
		t.Fatal("低优先级条目在队列满时应被丢弃")
	}
}

func TestSynthesisFailureDoesNotBlockQueue(t *testing.T) {
	synth := &fakeSynth{failing: map[string]bool{"坏": true}}
	sink := newFakeSink()

	queue := newTestQueue(t, Config{Synth: synth, Sink: sink})

	queue.Enqueue(Item{Priority: PrioritySuperChat, Text: "坏"})
	queue.Enqueue(Item{Priority: PriorityDanmaku, Text: "好"})

	waitFor(t, func() bool { return len(sink.completedTexts()) == 1 }, "好的一段未播完")
	assertOrder(t, sink.completedTexts(), "好")

	if stats := queue.Stats(); stats.Failed != 1 {
		t.Errorf("失败计数不符合预期: %d", stats.Failed)
	}
}

func TestEmptyAudioCountsAsFailure(t *testing.T) {
	synth := &fakeSynth{}
	sink := newFakeSink()
	queue := newTestQueue(t, Config{Synth: synth, Sink: sink})

	// fakeSynth 对空文本返回长度为 0 的字节
	queue.Enqueue(Item{Priority: PriorityDanmaku, Text: ""})

	waitFor(t, func() bool { return queue.Stats().Failed == 1 }, "空音频未计为失败")
	if len(sink.completedTexts()) != 0 {
		t.Error("空音频不应投递")
	}
}

func TestEnqueueAfterCloseReturnsFalse(t *testing.T) {
	queue := newTestQueue(t, Config{Synth: &fakeSynth{}, Sink: newFakeSink()})

	queue.Close()
	queue.Close() // 重复关闭应幂等

	if queue.Enqueue(Item{Priority: PriorityDanmaku, Text: "一"}) {
		t.Error("关闭后入队应返回 false")
	}
}

func TestNewValidatesDependencies(t *testing.T) {
	if _, err := New(Config{Sink: newFakeSink()}); err == nil {
		t.Error("缺少 Synthesizer 应报错")
	}
	if _, err := New(Config{Synth: &fakeSynth{}}); err == nil {
		t.Error("缺少 Sink 应报错")
	}
}

func TestPriorityString(t *testing.T) {
	cases := map[Priority]string{
		PrioritySuperChat: "super_chat",
		PriorityGift:      "gift",
		PriorityDanmaku:   "danmaku",
		PriorityProactive: "proactive",
		PriorityIdle:      "idle",
		Priority(99):      "priority(99)",
	}
	for priority, want := range cases {
		if got := priority.String(); got != want {
			t.Errorf("优先级 %d 的字符串为 %q，期望 %q", int(priority), got, want)
		}
	}
}
