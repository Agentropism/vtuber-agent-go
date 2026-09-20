package tts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agentropism/vtuber-agent-go/internal/logger"
)

// PCM 契约：所有引擎统一返回「裸 PCM16 小端、24kHz、单声道」字节流。
//
// 不带 WAV/RIFF 之类的容器头——格式由这三个常量约定，不随每次负载传递。
const (
	SampleRate     = 24000 // 采样率（Hz）
	Channels       = 1     // 声道数
	BytesPerSample = 2     // 位深 16 bit
)

// Engine 是一个云 TTS 引擎：把单句文本合成成裸 PCM。
//
// 实现必须保证返回字节符合 SampleRate / BytesPerSample / Channels；
// 引擎原生采样率不一致时，在实现内用 EnsureContract 转换。
type Engine interface {
	// Name 返回引擎名，用于日志与错误信息。
	Name() string
	// Synthesize 合成单句文本。失败返回错误，由上层决定是否降级到下一个引擎。
	Synthesize(ctx context.Context, text string) ([]byte, error)
}

// Chain 是一组按序降级的引擎。
//
// 构造后只读，可并发使用。
type Chain struct {
	engines []Engine
}

// NewChain 按给定顺序构造降级链，跳过 nil 引擎。
func NewChain(engines ...Engine) *Chain {
	kept := make([]Engine, 0, len(engines))
	for _, engine := range engines {
		if engine != nil {
			kept = append(kept, engine)
		}
	}
	return &Chain{engines: kept}
}

// Engines 返回链中的引擎副本，供日志与诊断使用。
func (c *Chain) Engines() []Engine {
	out := make([]Engine, len(c.engines))
	copy(out, c.engines)
	return out
}

// Names 返回链中各引擎的名字，按降级顺序。
func (c *Chain) Names() []string {
	out := make([]string, 0, len(c.engines))
	for _, engine := range c.engines {
		out = append(out, engine.Name())
	}
	return out
}

// Synthesize 依次尝试链上的引擎，返回首个成功的结果。
//
// 引擎报错或返回空音频都算失败，继续降级；ctx 取消时立即停止尝试。
// 全部失败时返回聚合错误，可用 errors.Is / errors.As 逐个检查。
func (c *Chain) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if len(c.engines) == 0 {
		return nil, errors.New("tts: 引擎链为空")
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("tts: 待合成文本为空")
	}

	failures := make([]error, 0, len(c.engines))

	for _, engine := range c.engines {
		if err := ctx.Err(); err != nil {
			failures = append(failures, fmt.Errorf("上下文已取消: %w", err))
			break
		}

		pcm, err := engine.Synthesize(ctx, text)
		if err != nil {
			logger.Warnf("TTS 引擎 %s 合成失败，降级到下一个: %v", engine.Name(), err)
			failures = append(failures, fmt.Errorf("%s: %w", engine.Name(), err))
			continue
		}

		// 空音频按失败处理：部分引擎存在「没有音频也不报错」的返回路径。
		if len(pcm) == 0 {
			logger.Warnf("TTS 引擎 %s 返回空音频，降级到下一个", engine.Name())
			failures = append(failures, fmt.Errorf("%s: 返回空音频", engine.Name()))
			continue
		}

		return pcm, nil
	}

	return nil, fmt.Errorf("tts: 全部引擎失败: %w", errors.Join(failures...))
}
