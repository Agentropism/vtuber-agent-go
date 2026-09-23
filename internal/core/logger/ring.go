package logger

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// DefaultCapacity 是环形缓冲容量：保留最近 500 条。
const DefaultCapacity = 500

// DefaultQueryLimit 是查询默认返回的条数。
const DefaultQueryLimit = 200

// Entry 是缓冲里的一条日志：只保留展示所需的最小字段。
type Entry struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Msg   string    `json:"msg"`
}

// RingBuffer 是固定容量的日志环形缓冲：写满后覆盖最旧的一条。
type RingBuffer struct {
	mu      sync.Mutex
	entries []Entry
	next    int  // 下一条写入位置
	full    bool // 是否已绕过一圈
}

// NewRingBuffer 构造环形缓冲；capacity <= 0 时取 DefaultCapacity。
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}

	return &RingBuffer{entries: make([]Entry, capacity)}
}

// Add 写入一条日志。
func (r *RingBuffer) Add(entry Entry) {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries[r.next] = entry
	r.next = (r.next + 1) % len(r.entries)
	if r.next == 0 {
		r.full = true
	}
}

// Snapshot 返回最近 limit 条日志（新的在前）；minLevel 非空时只保留不低于该级别的条目。
func (r *RingBuffer) Snapshot(limit int, minLevel string) []Entry {
	if limit <= 0 {
		limit = DefaultQueryLimit
	}

	threshold := levelRank(minLevel)
	filter := threshold >= 0

	r.mu.Lock()
	defer r.mu.Unlock()

	if limit > len(r.entries) {
		limit = len(r.entries)
	}

	size := r.next
	if r.full {
		size = len(r.entries)
	}

	out := make([]Entry, 0, limit)
	for i := 0; i < size && len(out) < limit; i++ {
		index := r.next - 1 - i
		if index < 0 {
			index += len(r.entries)
		}

		entry := r.entries[index]
		if filter && levelRank(entry.Level) < threshold {
			continue
		}
		out = append(out, entry)
	}

	return out
}

// levelRank 把级别名折算成可比较的序号；未知级别返回 -1。
func levelRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn", "warning":
		return 2
	case "error":
		return 3
	default:
		return -1
	}
}

// defaultRing 是进程级日志缓冲，ProvideLogger 装配的 handler 往里写，/api/logs 从这里读。
var defaultRing = NewRingBuffer(DefaultCapacity)

// Recent 返回进程内最近的日志（新的在前），供 /api/logs 使用。
func Recent(limit int, minLevel string) []Entry {
	return defaultRing.Snapshot(limit, minLevel)
}

// ringHandler 在把日志交给内层 handler 的同时记一份到环形缓冲。
type ringHandler struct {
	inner slog.Handler
	ring  *RingBuffer
}

func (h *ringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *ringHandler) Handle(ctx context.Context, record slog.Record) error {
	h.ring.Add(Entry{Time: record.Time, Level: record.Level.String(), Msg: record.Message})

	return h.inner.Handle(ctx, record)
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ringHandler{inner: h.inner.WithAttrs(attrs), ring: h.ring}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{inner: h.inner.WithGroup(name), ring: h.ring}
}
