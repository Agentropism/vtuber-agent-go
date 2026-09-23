package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/backend/web"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
	"github.com/Agentropism/vtuber-agent-go/internal/core/tts"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// errBroadcastQueueFull 是队列拒收注入条目的原因，会作为 503 的错误信息返回。
var errBroadcastQueueFull = errors.New("播报队列已满，注入内容被丢弃")

// provideBroadcast 装配 TTS 引擎链与统一播报队列。
//
// 未配置 [tts].engines 时返回 nil：此时会话只做文本下行，不产生语音播报。
// 投递目标由 pickSink 决定：推流与浏览器可以并存（扇出），都没有时退回只记日志的占位实现。
func provideBroadcast(cfg *config.Config, front *web.Frontend, streaming *streamRuntime) (*broadcast.Queue, error) {
	if len(cfg.TTS.Engines) == 0 {
		logger.Info("未配置 [tts].engines，跳过语音播报")
		return nil, nil
	}

	engines := make([]tts.Engine, 0, len(cfg.TTS.Engines))
	for _, name := range cfg.TTS.Engines {
		engine, err := buildTTSEngine(cfg, name)
		if err != nil {
			return nil, err
		}
		engines = append(engines, engine)
	}
	chain := tts.NewChain(engines...)

	sink := pickSink(front, streaming)

	queue, err := broadcast.New(broadcast.Config{
		Synth:       chain,
		Sink:        sink,
		Concurrency: cfg.Broadcast.Concurrency,
		MaxPending:  cfg.Broadcast.MaxPending,
		MinInterval: cfg.Broadcast.MinInterval.Std(),
	})
	if err != nil {
		return nil, fmt.Errorf("创建播报队列: %w", err)
	}

	logger.Infof("语音播报已启用: 引擎降级顺序=%v", chain.Names())
	return queue, nil
}

// provideSpeak 装配播报注入：把 POST /api/speak 的文本交给统一播报队列。
//
// 注入条目固定走 PriorityProactive——它不是观众带来的事件，属于「非事件驱动的话」，
// 因而排在弹幕、礼物与醒目留言之后，待机发言之前。要调档位改这里的取值即可。
// 队列的满/拒绝与抢占由 broadcast 自己记录日志，这里不重复记。
func provideSpeak(queue *broadcast.Queue) func(text, emotion string) error {
	return func(text, emotion string) error {
		if !queue.Enqueue(broadcast.Item{
			Priority: broadcast.PriorityProactive,
			Text:     text,
			Emotion:  emotion,
			Source:   "inject",
		}) {
			return errBroadcastQueueFull
		}
		return nil
	}
}

// buildTTSEngine 按配置构造单个 TTS 引擎。
func buildTTSEngine(cfg *config.Config, name string) (tts.Engine, error) {
	switch name {
	case "edge_tts":
		engine, err := tts.NewEdgeEngine(tts.EdgeConfig{
			Voice:          cfg.TTS.Edge.Voice,
			Rate:           cfg.TTS.Edge.Rate,
			Volume:         cfg.TTS.Edge.Volume,
			Pitch:          cfg.TTS.Edge.Pitch,
			Proxy:          cfg.TTS.Edge.Proxy,
			ReceiveTimeout: cfg.TTS.Edge.ReceiveTimeout,
		})
		return wrapEngineError(name, engine, err)

	case "openai_tts":
		engine, err := tts.NewOpenAICompatibleEngine(tts.OpenAICompatibleConfig{
			Name:    name,
			BaseURL: cfg.TTS.OpenAI.BaseURL,
			APIKey:  cfg.TTS.OpenAI.APIKey,
			Model:   cfg.TTS.OpenAI.Model,
			Voice:   cfg.TTS.OpenAI.Voice,
			Speed:   cfg.TTS.OpenAI.Speed,
		})
		return wrapEngineError(name, engine, err)

	case "siliconflow_tts":
		engine, err := tts.NewOpenAICompatibleEngine(tts.OpenAICompatibleConfig{
			Name:       name,
			BaseURL:    cfg.TTS.SiliconFlow.BaseURL,
			APIKey:     cfg.TTS.SiliconFlow.APIKey,
			Model:      cfg.TTS.SiliconFlow.Model,
			Voice:      cfg.TTS.SiliconFlow.Voice,
			Speed:      cfg.TTS.SiliconFlow.Speed,
			SampleRate: cfg.TTS.SiliconFlow.SampleRate,
			Gain:       cfg.TTS.SiliconFlow.Gain,
		})
		return wrapEngineError(name, engine, err)

	case "fish_api_tts":
		engine, err := tts.NewFishEngine(tts.FishConfig{
			BaseURL:     cfg.TTS.Fish.BaseURL,
			APIKey:      cfg.TTS.Fish.APIKey,
			Model:       cfg.TTS.Fish.Model,
			ReferenceID: cfg.TTS.Fish.ReferenceID,
			Latency:     cfg.TTS.Fish.Latency,
			SampleRate:  cfg.TTS.Fish.SampleRate,
		})
		return wrapEngineError(name, engine, err)

	case "minimax_tts":
		engine, err := tts.NewMinimaxEngine(tts.MinimaxConfig{
			BaseURL:    cfg.TTS.Minimax.BaseURL,
			GroupID:    cfg.TTS.Minimax.GroupID,
			APIKey:     cfg.TTS.Minimax.APIKey,
			Model:      cfg.TTS.Minimax.Model,
			VoiceID:    cfg.TTS.Minimax.VoiceID,
			SampleRate: cfg.TTS.Minimax.SampleRate,
		})
		return wrapEngineError(name, engine, err)

	default:
		return nil, fmt.Errorf(
			"未知的 TTS 引擎 %q（可选：edge_tts / openai_tts / siliconflow_tts / fish_api_tts / minimax_tts）",
			name,
		)
	}
}

// CheckTTSEngines 校验 [tts].engines：引擎名认不认识、按真实构造函数建不建得起来。
//
// 与装配走同一个 buildTTSEngine，所以它报的就是启动时会报的问题；doctor 拿它把
// 「TTS 配错了」提前到启动之前——不联网，也不真的去合成。
func CheckTTSEngines(cfg *config.Config) error {
	var problems []string
	for _, name := range cfg.TTS.Engines {
		if _, err := buildTTSEngine(cfg, name); err != nil {
			problems = append(problems, err.Error())
		}
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "；"))
	}

	return nil
}

// wrapEngineError 给引擎构造错误补上引擎名，便于定位是哪一项配置有问题。
func wrapEngineError(name string, engine tts.Engine, err error) (tts.Engine, error) {
	if err != nil {
		return nil, fmt.Errorf("创建 TTS 引擎 %s: %w", name, err)
	}
	return engine, nil
}

// logSink 是没有前端时的占位投递目标。
//
// 它不真正播放，但会按音频时长等待，因此播报队列的抢占、冷却与并行合成
// 仍按真实时序运转；配了 [frontend] 时换成 agent/frontend 的真实 Sink。
type logSink struct{}

func (s *logSink) Play(ctx context.Context, item broadcast.Item, pcm []byte) error {
	seconds := float64(len(pcm)/tts.BytesPerSample) / float64(tts.SampleRate)
	logger.Infof("[播报] priority=%s source=%s emotion=%q 时长=%.2fs 文本=%s",
		item.Priority, item.Source, item.Emotion, seconds, item.Text)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(seconds * float64(time.Second))):
		return nil
	}
}

// pickSink 决定播报投递目标。
//
// 推流与浏览器是并行的两条路，不是二选一：Chrome 里那个页面要靠 speak 驱动口型
// 与字幕，而推流管道要的是同一段 PCM，两者同时启用时扇出。
func pickSink(front *web.Frontend, streaming *streamRuntime) broadcast.Sink {
	var sinks []broadcast.Sink
	if streaming != nil {
		sinks = append(sinks, newStreamSink(streaming))
	}
	if front != nil {
		sinks = append(sinks, front.Sink())
	}

	switch len(sinks) {
	case 0:
		return &logSink{}
	case 1:
		return sinks[0]
	default:
		return &fanOutSink{sinks: sinks}
	}
}

// fanOutSink 并行投递给多个目标，全部结束才返回。
//
// 并行而不是串行：每个目标都按音频时长推进，串起来等于把音频以半速灌进推流管道
// （听感上拖长、口型对不上），播报队列的冷却也会算错。
type fanOutSink struct {
	sinks []broadcast.Sink
}

func (s *fanOutSink) Play(ctx context.Context, item broadcast.Item, pcm []byte) error {
	// 结果走 channel 收集而不是共享切片：每个目标只往带缓冲的通道里写自己的那一份，
	// 构造上就没有共享内存可竞争。
	results := make(chan error, len(s.sinks))
	for _, sink := range s.sinks {
		go func(sink broadcast.Sink) { results <- sink.Play(ctx, item, pcm) }(sink)
	}

	var firstErr error
	for range s.sinks {
		if err := <-results; err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
