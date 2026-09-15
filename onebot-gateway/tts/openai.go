package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// responseFormatPCM 是向 OpenAI 兼容端点索要的响应格式：裸 PCM，无需解码与去头。
const responseFormatPCM = "pcm"

// defaultHTTPTimeout 是 HTTP 类引擎的默认超时。
const defaultHTTPTimeout = 60 * time.Second

// OpenAICompatibleConfig 描述一个 OpenAI 兼容的 /audio/speech 端点。
//
// openai_tts 与 siliconflow_tts 共用这一个实现：两者协议形状相同，
// 差异只在默认值与少量字段——SiliconFlow 额外接受 sample_rate 与 gain。
type OpenAICompatibleConfig struct {
	Name       string  // 引擎名，用于日志与错误信息
	BaseURL    string  // API 根，例如 https://api.openai.com/v1
	APIKey     string  // 为空时不发 Authorization 头
	Model      string  // 例如 tts-1 / FunAudioLLM/CosyVoice2-0.5B
	Voice      string  // 例如 alloy / speech:Dreamflowers:...
	Speed      float64 // 0 表示不带该字段
	SampleRate int     // 非 0 时随 body 下发 sample_rate（SiliconFlow 支持）
	Gain       float64 // 非 0 时随 body 下发 gain（SiliconFlow 支持）

	// NativeSampleRate 是该端点 PCM 输出的采样率，<=0 时按契约值处理。
	NativeSampleRate int
	Timeout          time.Duration
}

// openAISpeechRequest 是 /audio/speech 的请求体。
type openAISpeechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format"`
	Speed          float64 `json:"speed,omitempty"`
	SampleRate     int     `json:"sample_rate,omitempty"`
	Gain           float64 `json:"gain,omitempty"`
}

type openAICompatibleEngine struct {
	cfg        OpenAICompatibleConfig
	endpoint   string
	httpClient *http.Client
}

// NewOpenAICompatibleEngine 构造 OpenAI 兼容引擎。
func NewOpenAICompatibleEngine(cfg OpenAICompatibleConfig) (Engine, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("tts: OpenAI 兼容引擎缺少 base_url")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("tts: OpenAI 兼容引擎缺少 model")
	}
	if strings.TrimSpace(cfg.Voice) == "" {
		return nil, errors.New("tts: OpenAI 兼容引擎缺少 voice")
	}
	if cfg.Name == "" {
		cfg.Name = "openai_compatible_tts"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultHTTPTimeout
	}
	if cfg.NativeSampleRate <= 0 {
		cfg.NativeSampleRate = SampleRate
	}

	return &openAICompatibleEngine{
		cfg:        cfg,
		endpoint:   strings.TrimSuffix(cfg.BaseURL, "/") + "/audio/speech",
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (e *openAICompatibleEngine) Name() string { return e.cfg.Name }

// Synthesize 向端点索要 PCM 音频。
func (e *openAICompatibleEngine) Synthesize(ctx context.Context, text string) ([]byte, error) {
	payload, err := json.Marshal(openAISpeechRequest{
		Model:          e.cfg.Model,
		Input:          text,
		Voice:          e.cfg.Voice,
		ResponseFormat: responseFormatPCM,
		Speed:          e.cfg.Speed,
		SampleRate:     e.cfg.SampleRate,
		Gain:           e.cfg.Gain,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化 %s 请求: %w", e.cfg.Name, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("构造 %s 请求: %w", e.cfg.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/pcm, application/octet-stream")
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s: %w", e.cfg.Name, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%s 返回 HTTP %d: %s", e.cfg.Name, resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	pcm, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 响应: %w", e.cfg.Name, err)
	}

	return EnsureContract(pcm, e.cfg.NativeSampleRate), nil
}

// NewOpenAITTSEngine 是 OpenAI 官方语音端点的预设。
func NewOpenAITTSEngine(apiKey, model, voice string) (Engine, error) {
	return NewOpenAICompatibleEngine(OpenAICompatibleConfig{
		Name:    "openai_tts",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  apiKey,
		Model:   model,
		Voice:   voice,
	})
}
