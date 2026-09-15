package tts

import (
	"context"
	"errors"
	"fmt"

	edge "github.com/wujunwei928/edge-tts-go/edge_tts"
)

// defaultEdgeTimeout 是 edge 引擎的拨号与接收超时（秒）。
const defaultEdgeTimeout = 30

// EdgeConfig 是 edge_tts 引擎的配置。
type EdgeConfig struct {
	Voice          string // 音色，例如 zh-CN-XiaoxiaoNeural
	Rate           string // 语速，例如 "+0%"
	Volume         string // 音量，例如 "+0%"
	Pitch          string // 音调，例如 "+0Hz"
	Proxy          string // 显式代理；为空时由底层库读环境变量
	ReceiveTimeout int    // 超时秒数，<=0 取默认 30
}

type edgeEngine struct {
	cfg EdgeConfig
}

// NewEdgeEngine 构造 edge_tts 引擎。
//
// edge 是微软 Edge 朗读服务的社区逆向协议（WebSocket + Sec-MS-GEC 签名），
// 输出 24kHz 单声道 mp3：采样率与契约一致，因此只需解码、不需重采样。
func NewEdgeEngine(cfg EdgeConfig) (Engine, error) {
	if cfg.Voice == "" {
		return nil, errors.New("tts: edge 引擎缺少 voice")
	}
	if cfg.ReceiveTimeout <= 0 {
		cfg.ReceiveTimeout = defaultEdgeTimeout
	}
	return &edgeEngine{cfg: cfg}, nil
}

func (e *edgeEngine) Name() string { return "edge_tts" }

// Synthesize 合成并解码为契约 PCM。
//
// 底层库的 Stream() 不支持 ctx 取消（其 context 只用于拨号超时），
// 这里用 goroutine 兜底：ctx 取消时立即返回错误，底层调用会在接收超时后自行结束。
func (e *edgeEngine) Synthesize(ctx context.Context, text string) ([]byte, error) {
	type result struct {
		audio []byte
		err   error
	}

	done := make(chan result, 1)
	go func() {
		audio, err := e.request(text)
		done <- result{audio: audio, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("edge 合成被取消: %w", ctx.Err())
	case got := <-done:
		if got.err != nil {
			return nil, got.err
		}
		if len(got.audio) == 0 {
			// 该库 Stream() 内部有一处窄窗口竞态，可能返回「空音频且无错误」，按失败处理
			return nil, errors.New("edge 返回空音频")
		}
		return got.audio, nil
	}
}

// request 调底层库完成一次合成，并转成契约 PCM。
func (e *edgeEngine) request(text string) ([]byte, error) {
	options := []edge.CommunicateOption{
		edge.SetVoice(e.cfg.Voice),
		edge.SetReceiveTimeout(e.cfg.ReceiveTimeout),
	}
	if e.cfg.Rate != "" {
		options = append(options, edge.SetRate(e.cfg.Rate))
	}
	if e.cfg.Volume != "" {
		options = append(options, edge.SetVolume(e.cfg.Volume))
	}
	if e.cfg.Pitch != "" {
		options = append(options, edge.SetPitch(e.cfg.Pitch))
	}
	if e.cfg.Proxy != "" {
		options = append(options, edge.SetProxy(e.cfg.Proxy))
	}

	communicate, err := edge.NewCommunicate(text, options...)
	if err != nil {
		return nil, fmt.Errorf("构造 edge 请求: %w", err)
	}

	audio, err := communicate.Stream()
	if err != nil {
		return nil, fmt.Errorf("edge 合成失败: %w", err)
	}

	pcm, rate, err := DecodeMP3(audio)
	if err != nil {
		return nil, fmt.Errorf("解码 edge 音频: %w", err)
	}

	return EnsureContract(pcm, rate), nil
}
