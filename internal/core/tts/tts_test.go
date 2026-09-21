package tts

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeEngine struct {
	name  string
	pcm   []byte
	err   error
	calls int
}

func (e *fakeEngine) Name() string { return e.name }

func (e *fakeEngine) Synthesize(ctx context.Context, text string) ([]byte, error) {
	e.calls++
	return e.pcm, e.err
}

func TestChainFallsBackOnError(t *testing.T) {
	first := &fakeEngine{name: "first", err: errors.New("炸了")}
	second := &fakeEngine{name: "second", pcm: []byte{1, 2, 3, 4}}

	pcm, err := NewChain(first, second).Synthesize(context.Background(), "你好")
	if err != nil {
		t.Fatalf("应降级成功: %v", err)
	}
	if len(pcm) != 4 {
		t.Errorf("返回内容不符合预期: %v", pcm)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Errorf("调用次数不符合预期: %d / %d", first.calls, second.calls)
	}
}

// TestChainTreatsEmptyAudioAsFailure 针对「没有音频也不报错」的引擎返回路径。
func TestChainTreatsEmptyAudioAsFailure(t *testing.T) {
	first := &fakeEngine{name: "silent"}
	second := &fakeEngine{name: "second", pcm: []byte{9, 9}}

	pcm, err := NewChain(first, second).Synthesize(context.Background(), "你好")
	if err != nil {
		t.Fatalf("空音频应触发降级: %v", err)
	}
	if len(pcm) != 2 {
		t.Errorf("应取第二个引擎的结果: %v", pcm)
	}
}

func TestChainReturnsAggregatedError(t *testing.T) {
	first := &fakeEngine{name: "a", err: errors.New("甲失败")}
	second := &fakeEngine{name: "b", err: errors.New("乙失败")}

	_, err := NewChain(first, second).Synthesize(context.Background(), "你好")
	if err == nil {
		t.Fatal("全部失败时应报错")
	}
	if !strings.Contains(err.Error(), "甲失败") || !strings.Contains(err.Error(), "乙失败") {
		t.Errorf("聚合错误应包含每一家的原因: %v", err)
	}
}

func TestChainStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	first := &fakeEngine{name: "a", err: errors.New("失败")}
	second := &fakeEngine{name: "b", pcm: []byte{1, 2}}

	if _, err := NewChain(first, second).Synthesize(ctx, "你好"); err == nil {
		t.Fatal("ctx 取消后应报错")
	}
	if first.calls != 0 || second.calls != 0 {
		t.Errorf("ctx 取消后不应再调用引擎，实际 %d / %d", first.calls, second.calls)
	}
}

func TestChainValidation(t *testing.T) {
	if _, err := NewChain().Synthesize(context.Background(), "你好"); err == nil {
		t.Error("空引擎链应报错")
	}
	if _, err := NewChain(&fakeEngine{name: "a"}).Synthesize(context.Background(), "   "); err == nil {
		t.Error("空文本应报错")
	}
}

func TestNewChainSkipsNilAndReportsNames(t *testing.T) {
	chain := NewChain(nil, &fakeEngine{name: "a"}, nil, &fakeEngine{name: "b"})

	names := chain.Names()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("引擎名不符合预期: %v", names)
	}
	if len(chain.Engines()) != 2 {
		t.Errorf("引擎数量不符合预期: %d", len(chain.Engines()))
	}
}
