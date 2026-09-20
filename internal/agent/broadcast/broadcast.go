package broadcast

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/logger"
)

// 默认参数。
const (
	defaultConcurrency = 2
	defaultMaxPending  = 32

	// preemptWaitTimeout 是抢占时等待旧播报退出的上限。
	// Sink.Play 理应在 ctx 取消后立即返回；超时说明实现没有遵守契约。
	preemptWaitTimeout = 2 * time.Second
)

// Priority 是播报优先级，数值越大越优先。
type Priority int

// 优先级分层：SC > 礼物 > 弹幕 > LLM 主动 > 待机。
const (
	PriorityIdle      Priority = iota // 待机发言：最低，任何来源都可打断
	PriorityProactive                 // LLM 主动互动
	PriorityDanmaku                   // 弹幕回复
	PriorityGift                      // 礼物答谢
	PrioritySuperChat                 // 醒目留言：最高
)

func (p Priority) String() string {
	switch p {
	case PrioritySuperChat:
		return "super_chat"
	case PriorityGift:
		return "gift"
	case PriorityDanmaku:
		return "danmaku"
	case PriorityProactive:
		return "proactive"
	case PriorityIdle:
		return "idle"
	default:
		return fmt.Sprintf("priority(%d)", int(p))
	}
}

// Item 是一条待播报内容。
type Item struct {
	Priority Priority
	Text     string
	// Emotion 是 Live2D 表情标签（模型 emo_map 的英文名，如 "joy"），空表示不换表情。
	// 队列只负责把它透传给 Sink，不校验取值：合法标签由具体模型决定，网关不认识模型。
	Emotion string
	Source  string // 来源标识，仅用于日志，例如 "bilibili:room_123" 或 "inject"
}

// Synthesizer 把文本合成成裸 PCM；tts.Chain 天然满足这个接口。
type Synthesizer interface {
	Synthesize(ctx context.Context, text string) ([]byte, error)
}

// Sink 接收合成好的音频并投递到前端。
//
// Play 返回即表示这一段播报结束；ctx 被取消表示该段被更高优先级打断，
// 实现必须在 ctx 取消后尽快返回，否则会拖住调度。
type Sink interface {
	Play(ctx context.Context, item Item, pcm []byte) error
}

// Config 是队列配置。
type Config struct {
	Synth Synthesizer
	Sink  Sink

	Concurrency int           // 并行合成上限，<=0 取 2
	MaxPending  int           // 待合成条目上限，<=0 取 32；超出时淘汰最低优先级
	MinInterval time.Duration // 两次播报之间的最小间隔（冷却），0 表示不限
}

// Stats 是累计计数，供观测与测试使用。
type Stats struct {
	Enqueued  uint64 // 成功入队
	Dropped   uint64 // 因容量或关闭被丢弃
	Played    uint64 // 完成播报
	Preempted uint64 // 被高优先级打断
	Failed    uint64 // 合成或播报失败
}

// entry 是队列内部条目。
type entry struct {
	item Item
	seq  uint64 // 入队序号，用于同级 FIFO
	pcm  []byte // 合成结果
}

// Queue 是统一播报队列。
//
// 单一调度协程持有全部决策状态：排序、抢占、冷却与投递顺序；
// 合成在独立协程中并行进行，因此下一段音频通常已经就绪。
type Queue struct {
	cfg  Config
	wake chan struct{} // 容量 1 的唤醒信号
	done chan struct{}
	wg   sync.WaitGroup

	mu      sync.Mutex
	pending []*entry // 待合成
	seq     uint64
	closed  bool

	enqueued  atomic.Uint64
	dropped   atomic.Uint64
	played    atomic.Uint64
	preempted atomic.Uint64
	failed    atomic.Uint64
}

// New 构造并启动播报队列。
func New(cfg Config) (*Queue, error) {
	if cfg.Synth == nil {
		return nil, errors.New("broadcast: 缺少 Synthesizer")
	}
	if cfg.Sink == nil {
		return nil, errors.New("broadcast: 缺少 Sink")
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaultConcurrency
	}
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = defaultMaxPending
	}
	if cfg.MinInterval < 0 {
		cfg.MinInterval = 0
	}

	q := &Queue{
		cfg:  cfg,
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}

	q.wg.Add(1)
	go q.run()

	return q, nil
}

// Enqueue 入队一条播报，非阻塞。
//
// 返回 false 表示被丢弃：队列已关闭，或队列已满且本条优先级不高于最低的待合成条目。
func (q *Queue) Enqueue(item Item) bool {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return false
	}

	if len(q.pending) >= q.cfg.MaxPending {
		victim := lowestIndex(q.pending)
		if victim < 0 || q.pending[victim].item.Priority >= item.Priority {
			q.mu.Unlock()
			q.dropped.Add(1)
			logger.Warnf("播报队列已满，丢弃新条目: priority=%s source=%s", item.Priority, item.Source)
			return false
		}
		logger.Warnf("播报队列已满，淘汰最低优先级待合成条目: priority=%s", q.pending[victim].item.Priority)
		q.pending = removeAt(q.pending, victim)
		q.dropped.Add(1)
	}

	q.seq++
	q.pending = append(q.pending, &entry{item: item, seq: q.seq})
	q.mu.Unlock()

	q.enqueued.Add(1)
	q.signal()
	return true
}

// Close 停止队列并等待在途任务结束；可重复调用。
func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	close(q.done)
	q.mu.Unlock()

	q.wg.Wait()
}

// Stats 返回累计计数快照。
func (q *Queue) Stats() Stats {
	return Stats{
		Enqueued:  q.enqueued.Load(),
		Dropped:   q.dropped.Load(),
		Played:    q.played.Load(),
		Preempted: q.preempted.Load(),
		Failed:    q.failed.Load(),
	}
}

// signal 唤醒调度协程；已有待处理信号时直接返回。
func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// synthResult 是一次合成的结果。
type synthResult struct {
	entry *entry
	pcm   []byte
	err   error
}

// run 是唯一的调度循环。
//
// 每轮按当前状态决定动作：先判抢占，再判投递，最后提交合成；
// 三者都无事可做时才阻塞等待外部事件。
func (q *Queue) run() {
	defer q.wg.Done()

	synthDone := make(chan synthResult, q.cfg.Concurrency)
	ready := make(map[uint64]*entry) // 已合成待投递，键为序号
	var inflight []*entry            // 已提交合成但尚未返回的条目

	var (
		playing    *entry
		playCancel context.CancelFunc
		playDone   chan struct{} // playing 为 nil 时必须为 nil，否则 select 会空转
		endedAt    time.Time     // 上一段播报结束时刻，用于冷却
		coolTimer  *time.Timer
		coolC      <-chan time.Time
	)

	for {
		q.mu.Lock()
		nextPending := bestPending(q.pending)
		pendingCount := len(q.pending)
		q.mu.Unlock()

		nextReady := bestReady(ready)
		// 尚未落地的条目也要参与判断：合成中的高优先级条目已经从 pending 里移除，
		// 若只看 pending，低优先级条目就会抢在它前面播出去。
		nextComing := bestOf(bestOf(inflight...), nextPending)

		topWaiting := Priority(-1)
		if highest := bestOf(nextReady, nextComing); highest != nil {
			topWaiting = highest.item.Priority
		}

		// 1) 抢占：待播内容里出现了比当前播报更高优先级的条目
		if playing != nil && topWaiting > playing.item.Priority {
			logger.Infof("高优先级播报打断当前条目: %s → %s", playing.item.Priority, topWaiting)
			q.preempted.Add(1)
			playCancel()

			select {
			case <-playDone:
			case <-time.After(preemptWaitTimeout):
				logger.Warn("Sink.Play 未在取消后及时返回，继续调度")
			}

			// 被打断的条目重新排队：保留已合成的音频，换新序号回到同级队尾
			if len(playing.pcm) > 0 {
				q.seq++
				playing.seq = q.seq
				ready[playing.seq] = playing
			}
			playing = nil
			playDone = nil
			endedAt = time.Now()
			continue
		}

		// 2) 投递：空闲且有已合成条目，且不抢在「更优的未落地条目」前面
		//
		// 门控用 better 而不是只比优先级：同优先级下先入队的条目若还在合成，
		// 后入队的不能先播出去，否则同级 FIFO 就破了。
		readyCanPlay := nextReady != nil && (nextComing == nil || !better(nextComing, nextReady))

		if playing == nil && readyCanPlay {
			wait := q.cfg.MinInterval - time.Since(endedAt)
			if wait > 0 {
				if coolC == nil {
					coolTimer = time.NewTimer(wait)
					coolC = coolTimer.C
				}
			} else {
				if coolTimer != nil {
					coolTimer.Stop()
					coolTimer, coolC = nil, nil
				}

				delete(ready, nextReady.seq)
				playing = nextReady
				playDone = make(chan struct{})

				ctx, cancel := context.WithCancel(context.Background())
				playCancel = cancel
				q.startPlay(ctx, nextReady, playDone)

				logger.Infof("开始播报: priority=%s source=%s 字节=%d",
					nextReady.item.Priority, nextReady.item.Source, len(nextReady.pcm))
				continue
			}
		}

		// 3) 合成：有空闲槽位且有待合成条目
		if len(inflight) < q.cfg.Concurrency && pendingCount > 0 {
			q.mu.Lock()
			index := indexOfBest(q.pending)
			submitted := q.pending[index]
			q.pending = removeAt(q.pending, index)
			q.mu.Unlock()

			inflight = append(inflight, submitted)
			go func(e *entry) {
				pcm, err := q.cfg.Synth.Synthesize(context.Background(), e.item.Text)
				synthDone <- synthResult{entry: e, pcm: pcm, err: err}
			}(submitted)
			continue
		}

		// 4) 无事可做：阻塞等待
		select {
		case <-q.wake:
		case result := <-synthDone:
			inflight = removeEntry(inflight, result.entry.seq)
			switch {
			case result.err != nil:
				q.failed.Add(1)
				logger.Errorf("播报合成失败: priority=%s 错误=%v", result.entry.item.Priority, result.err)
			case len(result.pcm) == 0:
				q.failed.Add(1)
				logger.Warnf("播报合成返回空音频: priority=%s", result.entry.item.Priority)
			default:
				result.entry.pcm = result.pcm
				ready[result.entry.seq] = result.entry
			}
		case <-playDone:
			playing = nil
			playDone = nil
			endedAt = time.Now()
		case <-coolC:
			coolTimer, coolC = nil, nil
		case <-q.done:
			if playCancel != nil {
				playCancel()
			}
			return
		}
	}
}

// startPlay 在独立协程里投递一段音频。
func (q *Queue) startPlay(ctx context.Context, e *entry, done chan struct{}) {
	go func() {
		defer close(done)

		err := q.cfg.Sink.Play(ctx, e.item, e.pcm)
		if err == nil {
			q.played.Add(1)
			return
		}
		if errors.Is(err, context.Canceled) {
			return // 被抢占，不计失败
		}
		q.failed.Add(1)
		logger.Errorf("播报投递失败: priority=%s 错误=%v", e.item.Priority, err)
	}()
}

// ---- 排序辅助：pending 规模很小（默认上限 32），线性扫描比手写堆更不容易出错 ----

// better 定义取优顺序：优先级降序，同级序号升序。
func better(a, b *entry) bool {
	if a.item.Priority != b.item.Priority {
		return a.item.Priority > b.item.Priority
	}
	return a.seq < b.seq
}

// indexOfBest 返回最优条目的下标，空切片返回 -1。
func indexOfBest(entries []*entry) int {
	best := -1
	for i, e := range entries {
		if best < 0 || better(e, entries[best]) {
			best = i
		}
	}
	return best
}

// bestPending 返回最优待合成条目，空队列返回 nil。
func bestPending(entries []*entry) *entry {
	if i := indexOfBest(entries); i >= 0 {
		return entries[i]
	}
	return nil
}

// bestReady 返回最优待投递条目，空集合返回 nil。
func bestReady(ready map[uint64]*entry) *entry {
	var best *entry
	for _, e := range ready {
		if best == nil || better(e, best) {
			best = e
		}
	}
	return best
}

// lowestIndex 返回优先级最低（同级则序号最大）的条目下标，空切片返回 -1。
func lowestIndex(entries []*entry) int {
	worst := -1
	for i, e := range entries {
		if worst < 0 || better(entries[worst], e) {
			worst = i
		}
	}
	return worst
}

// bestOf 返回若干可空条目中最优的一个，全为空时返回 nil。
func bestOf(entries ...*entry) *entry {
	var best *entry
	for _, e := range entries {
		if e == nil {
			continue
		}
		if best == nil || better(e, best) {
			best = e
		}
	}
	return best
}

// removeEntry 按序号移除条目，找不到时原样返回。
func removeEntry(entries []*entry, seq uint64) []*entry {
	for i, e := range entries {
		if e.seq == seq {
			return removeAt(entries, i)
		}
	}
	return entries
}

// removeAt 移除下标 i 的元素并返回新切片。
func removeAt(entries []*entry, i int) []*entry {
	if i < 0 || i >= len(entries) {
		return entries
	}
	return append(entries[:i], entries[i+1:]...)
}
