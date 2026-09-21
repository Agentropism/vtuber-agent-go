package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/backend/api"
	"github.com/Agentropism/vtuber-agent-go/internal/backend/web"
	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
)

// ttsPreviewTimeout 是试听合成的上限：试听是交互动作，卡住就该报错而不是挂着。
const ttsPreviewTimeout = 30 * time.Second

// ttsEngineNames 是支持的全部引擎，取值与 [tts].engines 一致。
var ttsEngineNames = []string{"edge_tts", "openai_tts", "siliconflow_tts", "fish_api_tts", "minimax_tts"}

// provideTTSInfo 汇总引擎与当前配置：前端要能看见配了哪些、能不能用、音色是什么。
//
// Ready 靠真的构造一次引擎来判断（构造函数便宜且不需要网络），失败原因原样带出去——
// 让「为什么不能用」在接口上可见，比前端自己猜要好。
func provideTTSInfo(cfg *config.Config) func() []api.TTSInfo {
	return func() []api.TTSInfo {
		enabled := make(map[string]bool, len(cfg.TTS.Engines))
		for _, name := range cfg.TTS.Engines {
			enabled[name] = true
		}

		views := make([]api.TTSInfo, 0, len(ttsEngineNames))
		for _, name := range ttsEngineNames {
			view := api.TTSInfo{
				Name:    name,
				Enabled: enabled[name],
				Voice:   configuredVoice(cfg, name),
				Model:   configuredModel(cfg, name),
			}

			if _, err := buildTTSEngine(cfg, name); err != nil {
				view.Reason = err.Error()
			} else {
				view.Ready = true
			}

			views = append(views, view)
		}

		return views
	}
}

// configuredVoice 返回该引擎当前配置的音色（各引擎字段名不同，映射集中在这里）。
func configuredVoice(cfg *config.Config, engine string) string {
	switch engine {
	case "edge_tts":
		return cfg.TTS.Edge.Voice
	case "openai_tts":
		return cfg.TTS.OpenAI.Voice
	case "siliconflow_tts":
		return cfg.TTS.SiliconFlow.Voice
	case "fish_api_tts":
		return cfg.TTS.Fish.ReferenceID
	case "minimax_tts":
		return cfg.TTS.Minimax.VoiceID
	default:
		return ""
	}
}

// configuredModel 返回该引擎当前配置的模型名。
func configuredModel(cfg *config.Config, engine string) string {
	switch engine {
	case "openai_tts":
		return cfg.TTS.OpenAI.Model
	case "siliconflow_tts":
		return cfg.TTS.SiliconFlow.Model
	case "fish_api_tts":
		return cfg.TTS.Fish.Model
	case "minimax_tts":
		return cfg.TTS.Minimax.Model
	default:
		return ""
	}
}

// applyVoiceOverride 把请求里的音色写进配置副本（空字符串表示不改）。
func applyVoiceOverride(cfg *config.Config, engine, voice string) {
	if strings.TrimSpace(voice) == "" {
		return
	}

	switch engine {
	case "edge_tts":
		cfg.TTS.Edge.Voice = voice
	case "openai_tts":
		cfg.TTS.OpenAI.Voice = voice
	case "siliconflow_tts":
		cfg.TTS.SiliconFlow.Voice = voice
	case "fish_api_tts":
		cfg.TTS.Fish.ReferenceID = voice
	case "minimax_tts":
		cfg.TTS.Minimax.VoiceID = voice
	}
}

// provideTTSPreview 装配试听：按请求的引擎与音色覆盖后合成，返回 WAV 字节。
//
// 不入播报队列：试听要能挑任意引擎/音色，而队列里的音色由配置决定；前端拿到音频
// 自己决定什么时候播（可能与正在播的内容重叠，这是有意识的取舍）。
func provideTTSPreview(cfg *config.Config) func(engine, voice, text string) ([]byte, error) {
	return func(engine, voice, text string) ([]byte, error) {
		name := strings.TrimSpace(engine)
		if name == "" {
			if len(cfg.TTS.Engines) == 0 {
				return nil, errors.New("未配置 [tts].engines")
			}
			name = cfg.TTS.Engines[0]
		}

		// 配置按值复制：覆盖音色只影响这次试听，不动进程里正在用的配置
		preview := *cfg
		applyVoiceOverride(&preview, name, voice)

		built, err := buildTTSEngine(&preview, name)
		if err != nil {
			return nil, err
		}

		ctx, cancel := context.WithTimeout(context.Background(), ttsPreviewTimeout)
		defer cancel()

		pcm, err := built.Synthesize(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("合成试听: %w", err)
		}

		return web.EncodeWAV(pcm), nil
	}
}
