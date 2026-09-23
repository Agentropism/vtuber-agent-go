package logger

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// 写满后覆盖最旧的，新的在前。
func TestRingBufferKeepsLatest(t *testing.T) {
	ring := NewRingBuffer(3)

	for i := 1; i <= 5; i++ {
		ring.Add(Entry{Time: time.Unix(int64(i), 0), Level: "INFO", Msg: fmt.Sprintf("第 %d 条", i)})
	}

	got := ring.Snapshot(10, "")
	if len(got) != 3 {
		t.Fatalf("条数 = %d, want 3", len(got))
	}
	want := []string{"第 5 条", "第 4 条", "第 3 条"}
	for i := range want {
		if got[i].Msg != want[i] {
			t.Fatalf("顺序不符合预期: %+v", got)
		}
	}
}

// limit 截断与级别过滤（不低于指定级别）。
func TestRingBufferLimitAndLevelFilter(t *testing.T) {
	ring := NewRingBuffer(10)

	ring.Add(Entry{Level: "DEBUG", Msg: "调试"})
	ring.Add(Entry{Level: "INFO", Msg: "信息"})
	ring.Add(Entry{Level: "WARN", Msg: "警告"})
	ring.Add(Entry{Level: "ERROR", Msg: "错误"})

	if got := ring.Snapshot(2, ""); len(got) != 2 || got[0].Msg != "错误" || got[1].Msg != "警告" {
		t.Fatalf("limit 截断不符合预期: %+v", got)
	}

	got := ring.Snapshot(10, "warn")
	if len(got) != 2 || got[0].Msg != "错误" || got[1].Msg != "警告" {
		t.Fatalf("级别过滤不符合预期: %+v", got)
	}

	if got := ring.Snapshot(10, "info"); len(got) != 3 {
		t.Fatalf("info 以上应有 3 条: %+v", got)
	}

	// 未知级别名视为不过滤
	if got := ring.Snapshot(10, "verbose"); len(got) != 4 {
		t.Fatalf("未知级别不应过滤: %+v", got)
	}
}

// 未写满时按实际条数返回，limit 超容量不放大分配。
func TestRingBufferPartialAndOversizedLimit(t *testing.T) {
	ring := NewRingBuffer(3)
	ring.Add(Entry{Level: "INFO", Msg: "唯一一条"})

	got := ring.Snapshot(1000, "")
	if len(got) != 1 || got[0].Msg != "唯一一条" {
		t.Fatalf("未写满时结果不符合预期: %+v", got)
	}
}

// 并发写入不应 panic 或串行错乱：写完后快照条数等于容量且时间戳非零。
func TestRingBufferConcurrent(t *testing.T) {
	ring := NewRingBuffer(64)

	const writers, perWriter = 8, 200
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				ring.Add(Entry{Level: "INFO", Msg: fmt.Sprintf("w%d-%d", worker, i)})
			}
		}(w)
	}
	wg.Wait()

	got := ring.Snapshot(0, "")
	if len(got) != 64 {
		t.Fatalf("快照条数 = %d, want 64", len(got))
	}
	for _, entry := range got {
		if entry.Time.IsZero() || entry.Msg == "" {
			t.Fatalf("存在不完整条目: %+v", entry)
		}
	}
}
