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

// defaultFishBaseURL 是 Fish Audio 的默认地址。
const defaultFishBaseURL = "https://api.fish.audio"

// FishConfig 是 fish_api_tts 引擎的配置。
//
// 一次性合成走 POST {BaseURL}/v1/tts。上游虽提供 Go SDK，但它把 WebSocket 流式
// 实现放在同一个包里，会连带引入 gorilla/websocket 与 msgpack；本项目只需要
// 一次性合成，因此直接手写这个普通 POST。
type FishConfig struct {
	BaseURL     string // 默认 https://api.fish.audio
	APIKey      string // 必填，Authorization: Bearer
	Model       string // 可选，例如 s1 / speech-1.6
	ReferenceID string // 音色模型 ID
	Latency     string // normal / balanced
	SampleRate  int    // <=0 时取契约值；Fish 支持在请求里直接指定采样率
	Timeout     time.Duration
}

type fishRequest struct {
	Text        string `json:"text"`
	Format      string `json:"format"`
	SampleRate  int    `json:"sample_rate,omitempty"`
	ReferenceID string `json:"reference_id,omitempty"`
	Model       string `json:"model,omitempty"`
	Latency     string `json:"latency,omitempty"`
}

type fishEngine struct {
	cfg        FishConfig
	endpoint   string
	httpClient *http.Client
}

// NewFishEngine 构造 Fish Audio 引擎。
func NewFishEngine(cfg FishConfig) (Engine, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("tts: fish 引擎缺少 api_key")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultFishBaseURL
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = SampleRate
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultHTTPTimeout
	}

	return &fishEngine{
		cfg:        cfg,
		endpoint:   strings.TrimSuffix(cfg.BaseURL, "/") + "/v1/tts",
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (e *fishEngine) Name() string { return "fish_api_tts" }

// Synthesize 向 Fish Audio 索要 PCM 音频。
func (e *fishEngine) Synthesize(ctx context.Context, text string) ([]byte, error) {
	payload, err := json.Marshal(fishRequest{
		Text:        text,
		Format:      responseFormatPCM,
		SampleRate:  e.cfg.SampleRate,
		ReferenceID: e.cfg.ReferenceID,
		Model:       e.cfg.Model,
		Latency:     e.cfg.Latency,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化 fish 请求: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("构造 fish 请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/pcm, application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 fish: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("fish 返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	pcm, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 fish 响应: %w", err)
	}

	return EnsureContract(pcm, e.cfg.SampleRate), nil
}
