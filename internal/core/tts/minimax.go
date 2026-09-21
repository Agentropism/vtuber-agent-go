package tts

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// minimax 相关默认值。
const (
	defaultMinimaxBaseURL = "https://api.minimax.chat"
	defaultMinimaxModel   = "speech-02-turbo"
	defaultMinimaxVoice   = "female-shaonv"
)

// MinimaxConfig 是 minimax_tts 引擎的配置。
//
// 端点 POST {BaseURL}/v1/t2a_v2?GroupId=<group_id>，GroupId 走查询参数，
// 鉴权走 Authorization 头；响应里的音频是 hex 编码。
// 这里用 stream=false 一次性取回整段音频，比原先 Python 实现逐行解析 SSE 更简单。
type MinimaxConfig struct {
	BaseURL    string // 默认 https://api.minimax.chat
	GroupID    string // 必填，作为查询参数
	APIKey     string // 必填
	Model      string // 默认 speech-02-turbo
	VoiceID    string // 默认 female-shaonv
	SampleRate int    // <=0 时取契约值
	Timeout    time.Duration
}

type minimaxVoiceSetting struct {
	VoiceID string  `json:"voice_id"`
	Speed   float64 `json:"speed"`
	Vol     float64 `json:"vol"`
	Pitch   int     `json:"pitch"`
}

type minimaxAudioSetting struct {
	SampleRate int    `json:"sample_rate"`
	Bitrate    int    `json:"bitrate"`
	Format     string `json:"format"`
	Channel    int    `json:"channel"`
}

type minimaxRequest struct {
	Model             string              `json:"model"`
	Text              string              `json:"text"`
	Stream            bool                `json:"stream"`
	VoiceSetting      minimaxVoiceSetting `json:"voice_setting"`
	AudioSetting      minimaxAudioSetting `json:"audio_setting"`
	PronunciationDict map[string]any      `json:"pronunciation_dict"`
}

type minimaxResponse struct {
	Data struct {
		Audio string `json:"audio"`
	} `json:"data"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

type minimaxEngine struct {
	cfg        MinimaxConfig
	endpoint   string
	httpClient *http.Client
}

// NewMinimaxEngine 构造 MiniMax 引擎。
func NewMinimaxEngine(cfg MinimaxConfig) (Engine, error) {
	if strings.TrimSpace(cfg.GroupID) == "" {
		return nil, errors.New("tts: minimax 引擎缺少 group_id")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("tts: minimax 引擎缺少 api_key")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultMinimaxBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = defaultMinimaxModel
	}
	if cfg.VoiceID == "" {
		cfg.VoiceID = defaultMinimaxVoice
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = SampleRate
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultHTTPTimeout
	}

	base := strings.TrimSuffix(cfg.BaseURL, "/") + "/v1/t2a_v2"
	query := url.Values{}
	query.Set("GroupId", cfg.GroupID)

	return &minimaxEngine{
		cfg:        cfg,
		endpoint:   base + "?" + query.Encode(),
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (e *minimaxEngine) Name() string { return "minimax_tts" }

// Synthesize 向 MiniMax 索要 PCM 音频并解码 hex。
func (e *minimaxEngine) Synthesize(ctx context.Context, text string) ([]byte, error) {
	payload, err := json.Marshal(minimaxRequest{
		Model:  e.cfg.Model,
		Text:   text,
		Stream: false,
		VoiceSetting: minimaxVoiceSetting{
			VoiceID: e.cfg.VoiceID,
			Speed:   1.0,
			Vol:     1.0,
			Pitch:   0,
		},
		AudioSetting: minimaxAudioSetting{
			SampleRate: e.cfg.SampleRate,
			Bitrate:    128000,
			Format:     responseFormatPCM,
			Channel:    Channels,
		},
		PronunciationDict: map[string]any{"tone": []string{}},
	})
	if err != nil {
		return nil, fmt.Errorf("序列化 minimax 请求: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("构造 minimax 请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 minimax: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 minimax 响应: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("minimax 返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed minimaxResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("解析 minimax 响应: %w", err)
	}
	if parsed.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("minimax 业务错误 %d: %s", parsed.BaseResp.StatusCode, parsed.BaseResp.StatusMsg)
	}
	if parsed.Data.Audio == "" {
		return nil, errors.New("minimax 响应中没有音频数据")
	}

	pcm, err := hex.DecodeString(parsed.Data.Audio)
	if err != nil {
		return nil, fmt.Errorf("解码 minimax hex 音频: %w", err)
	}

	return EnsureContract(pcm, e.cfg.SampleRate), nil
}
