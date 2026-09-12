package upload

import (
	"context"
	"testing"
	"time"
)

// initTest 以最小配置初始化上传包，并注册测试清理。
func initTest(t *testing.T, opts Options) {
	t.Helper()
	if opts.QueueSize <= 0 {
		opts.QueueSize = 4
	}
	if err := Init("ws://127.0.0.1:1/test", "qq", opts); err != nil {
		t.Fatalf("初始化上传: %v", err)
	}
	t.Cleanup(Shutdown)
}

func TestDispatchScopeGatesUpload(t *testing.T) {
	initTest(t, Options{})
	ctx := BeginDispatch(context.Background())
	upload(ctx, platformEvent{Type: EventTypeMessage, PlatformName: "qq", ContentText: "你好"})

	if len(writeCh) != 0 {
		t.Fatalf("分发未完成时不应入队: 缓存长度=%d", len(writeCh))
	}
	FinishDispatch(ctx)
	if len(writeCh) != 1 {
		t.Fatalf("分发完成后应入队: 缓存长度=%d", len(writeCh))
	}
}

func TestUploadWithoutScopeEnqueuesDirectly(t *testing.T) {
	initTest(t, Options{})
	upload(context.Background(), platformEvent{Type: EventTypeMessage, PlatformName: "qq", ContentText: "你好"})
	if len(writeCh) != 1 {
		t.Fatalf("无分发上下文时应直接入队: 缓存长度=%d", len(writeCh))
	}
}

func TestEnqueueBackpressureTimeoutDrops(t *testing.T) {
	initTest(t, Options{QueueSize: 1, QueueWaitTimeout: 50 * time.Millisecond})
	enqueue([]byte("占位")) // 占满缓存

	start := time.Now()
	enqueue([]byte("第二条"))
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("背压未生效: 入队耗时=%v", elapsed)
	}
	if len(writeCh) != 1 {
		t.Fatalf("超时后应丢弃: 缓存长度=%d", len(writeCh))
	}
}

func TestEnqueueBackpressureBlocksUntilShutdown(t *testing.T) {
	initTest(t, Options{QueueSize: 1, QueueWaitTimeout: 0})
	enqueue([]byte("占位")) // 占满缓存

	released := make(chan struct{})
	go func() {
		enqueue([]byte("第二条"))
		close(released)
	}()

	select {
	case <-released:
		t.Fatal("缓存满时背压应阻塞入队")
	case <-time.After(100 * time.Millisecond):
	}
	Shutdown()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("关闭后应解除阻塞")
	}
}

// TestEnqueueDropsWithoutConsumer 空目标（无消费端）时入队应非阻塞丢弃。
func TestEnqueueDropsWithoutConsumer(t *testing.T) {
	if err := Init("", "qq", Options{QueueSize: 1, QueueWaitTimeout: 0}); err != nil {
		t.Fatalf("初始化上传: %v", err)
	}
	t.Cleanup(Shutdown)

	enqueue([]byte("x"))
	if len(writeCh) != 0 {
		t.Fatalf("目标未配置时应丢弃不入队: 缓存长度=%d", len(writeCh))
	}
}
