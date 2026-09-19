package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/agent/frontend"
	"github.com/Agentropism/vtuber-agent-go/internal/config"
	"github.com/Agentropism/vtuber-agent-go/internal/gateway/server"
	"github.com/Agentropism/vtuber-agent-go/internal/tts"

	"go.uber.org/zap"
)

// errBroadcastQueueFull 是队列拒收注入条目的原因，会作为 503 的错误信息返回。
var errBroadcastQueueFull = errors.New("播报队列已满，注入内容被丢弃")

// provideBroadcast 装配 TTS 引擎链与统一播报队列。
//
// 未配置 [tts].engines 时返回 nil：此时会话只做文本下行，不产生语音播报。
// front 非空时，音频投递到浏览器；否则用只记日志的占位实现。
func provideBroadcast(cfg *config.Config, log *zap.Logger, front *frontend.Frontend) (*broadcast.Queue, error) {
	if len(cfg.TTS.Engines) == 0 {
		log.Sugar().Info("未配置 [tts].engines，跳过语音播报")
		return nil, nil
	}

	tts.SetLogger(log)
	broadcast.SetLogger(log)

	engines := make([]tts.Engine, 0, len(cfg.TTS.Engines))
	for _, name := range cfg.TTS.Engines {
		engine, err := buildTTSEngine(cfg, name)
		if err != nil {
			return nil, err
		}
		engines = append(engines, engine)
	}
	chain := tts.NewChain(engines...)

	var sink broadcast.Sink = &logSink{log: log}
	if front != nil {
		sink = front.Sink()
	}

	queue, err := broadcast.New(broadcast.Config{
		Synth:       chain,
		Sink:        sink,
		Concurrency: cfg.Broadcast.Concurrency,
		MaxPending:  cfg.Broadcast.MaxPending,
		MinInterval: cfg.Broadcast.MinInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("创建播报队列: %w", err)
	}

	log.Sugar().Infof("语音播报已启用: 引擎降级顺序=%v", chain.Names())
	return queue, nil
}

// provideInject 装配 /inject 的实现：把注入请求交给统一播报队列。
//
// 注入条目固定走 PriorityProactive——它不是观众带来的事件，属于「非事件驱动的话」，
// 因而排在弹幕、礼物与醒目留言之后，待机发言之前。要调档位改这里的取值即可。
// 队列的满/拒绝与抢占由 broadcast 自己记录日志，这里不重复记。
func provideInject(queue *broadcast.Queue) server.InjectFunc {
	return func(req server.InjectRequest) error {
		if !queue.Enqueue(broadcast.Item{
			Priority: broadcast.PriorityProactive,
			Text:     req.Text,
			Emotion:  req.Emotion,
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
type logSink struct{ log *zap.Logger }

func (s *logSink) Play(ctx context.Context, item broadcast.Item, pcm []byte) error {
	seconds := float64(len(pcm)/tts.BytesPerSample) / float64(tts.SampleRate)
	s.log.Sugar().Infof("[播报] priority=%s source=%s emotion=%q 时长=%.2fs 文本=%s",
		item.Priority, item.Source, item.Emotion, seconds, item.Text)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(seconds * float64(time.Second))):
		return nil
	}
}
