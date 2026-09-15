package upload

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// initWith 以最小配置初始化上传管线，并注册测试清理。
func initWith(t *testing.T, opts Options) {
	t.Helper()
	if opts.QueueSize <= 0 {
		opts.QueueSize = 4
	}
	if err := Init(opts); err != nil {
		t.Fatalf("初始化上传: %v", err)
	}
	t.Cleanup(Shutdown)
}

// recordHandler 注入一个把事件转发到通道的处理器。
func recordHandler(t *testing.T) <-chan []byte {
	t.Helper()

	received := make(chan []byte, 32)
	SetHandler(func(payload []byte) { received <- payload })
	t.Cleanup(func() { SetHandler(nil) })

	return received
}

// blockingHandler 注入一个接手事件后即阻塞的处理器，用于制造背压。
// 返回的 started 在处理器接手首个事件后收到通知。
func blockingHandler(t *testing.T) <-chan struct{} {
	t.Helper()

	release := make(chan struct{})
	started := make(chan struct{}, 1)
	SetHandler(func([]byte) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	})
	t.Cleanup(func() {
		close(release)
		SetHandler(nil)
	})

	return started
}

func assertReceived(t *testing.T, received <-chan []byte, wantText string) {
	t.Helper()

	select {
	case payload := <-received:
		var got platformEvent
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatalf("解析投递负载失败: %v", err)
		}
		if got.ContentText != wantText {
			t.Fatalf("投递内容不符合预期: %q，期望 %q", got.ContentText, wantText)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到投递事件")
	}
}

func TestDispatchScopeGatesUpload(t *testing.T) {
	received := recordHandler(t)
	initWith(t, Options{})

	ctx := BeginDispatch(context.Background())
	upload(ctx, platformEvent{Type: EventTypeMessage, PlatformName: "qq", ContentText: "你好"})

	select {
	case <-received:
		t.Fatal("分发未完成时不应投递")
	case <-time.After(50 * time.Millisecond):
	}

	FinishDispatch(ctx)
	assertReceived(t, received, "你好")
}

func TestUploadWithoutScopeDispatchesDirectly(t *testing.T) {
	received := recordHandler(t)
	initWith(t, Options{})

	upload(context.Background(), platformEvent{Type: EventTypeMessage, PlatformName: "qq", ContentText: "你好"})
	assertReceived(t, received, "你好")
}

func TestQueueBackpressureTimeoutDrops(t *testing.T) {
	started := blockingHandler(t)
	initWith(t, Options{QueueSize: 1, QueueWaitTimeout: 50 * time.Millisecond})

	enqueue([]byte("第一条"))
	<-started // 消费协程已接手，缓存重新空出
	enqueue([]byte("第二条"))

	start := time.Now()
	enqueue([]byte("第三条"))
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("背压未生效: 入队耗时=%v", elapsed)
	}
	if len(queue) != 1 {
		t.Fatalf("超时后应丢弃: 缓存长度=%d", len(queue))
	}
}

func TestQueueBackpressureBlocksUntilShutdown(t *testing.T) {
	started := blockingHandler(t)
	initWith(t, Options{QueueSize: 1, QueueWaitTimeout: 0})

	enqueue([]byte("第一条"))
	<-started
	enqueue([]byte("第二条")) // 占满缓存

	released := make(chan struct{})
	go func() {
		enqueue([]byte("第三条"))
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

// TestEnqueueDropsWithoutHandler 未注入终端处理器时入队应非阻塞丢弃。
func TestEnqueueDropsWithoutHandler(t *testing.T) {
	SetHandler(nil)
	initWith(t, Options{QueueSize: 1, QueueWaitTimeout: 0})

	enqueue([]byte("x"))
	if len(queue) != 0 {
		t.Fatalf("未注入处理器时应丢弃不入队: 缓存长度=%d", len(queue))
	}
}
