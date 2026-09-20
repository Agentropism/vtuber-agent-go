package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Log struct {
		Level string `mapstructure:"level"`
	} `mapstructure:"log"`
	Memory struct {
		QueueSize          int           `mapstructure:"queue_size"`           // 上传缓存容量，默认 256
		QueueWaitTimeout   time.Duration `mapstructure:"queue_wait_timeout"`   // 缓存满时等待上限，0=无限等待
		SensitiveWordsFile string        `mapstructure:"sensitive_words_file"` // 敏感词库文件路径，空=不启用
		DedupTTL           time.Duration `mapstructure:"dedup_ttl"`            // 去重窗口，0=不启用
	} `mapstructure:"memory"`
	Server struct {
		Addr string `mapstructure:"addr"`
	} `mapstructure:"server"`
	LLM struct {
		BaseURL     string  `mapstructure:"base_url"`    // OpenAI 兼容端点，例如 https://api.deepseek.com/v1
		APIKey      string  `mapstructure:"api_key"`     // 优先取环境变量 LLM_API_KEY
		Model       string  `mapstructure:"model"`       // 例如 deepseek-chat
		Temperature float64 `mapstructure:"temperature"` // 0 表示使用默认值 1.0
	} `mapstructure:"llm"`
	Agent struct {
		SystemPrompt  string        `mapstructure:"system_prompt"`   // 角色系统提示词，为空时用内置兜底值
		CharacterFile string        `mapstructure:"character_file"`  // 角色定义文件（TOML），非空时其 system_prompt 优先
		MaxToolRounds int           `mapstructure:"max_tool_rounds"` // 单轮对话内工具循环上限，0=默认 8
		InterruptMode string        `mapstructure:"interrupt_mode"`  // 打断标记角色：user（默认）/ system
		QueueSize     int           `mapstructure:"queue_size"`      // 每个会话的待处理消息上限
		TurnTimeout   time.Duration `mapstructure:"turn_timeout"`    // 单轮对话超时，例如 "60s"

		// 历史裁剪：两者同时生效，0 表示用默认值（12 轮 / 4000 token）。
		HistoryMaxTurns  int `mapstructure:"history_max_turns"`
		HistoryMaxTokens int `mapstructure:"history_max_tokens"`

		// MemoryFile 是长期记忆文件（JSON Lines），为空表示不启用。
		MemoryFile string `mapstructure:"memory_file"`
		// RecallLimit 是每轮召回的历史条数，0 表示用默认值 5。
		RecallLimit int `mapstructure:"recall_limit"`

		// EnableTools 打开工具调用（记忆检索、状态查询）。
		EnableTools bool `mapstructure:"enable_tools"`
		// IdleSpeakInterval > 0 时，渠道静默超过该时长就主动说一句，例如 "5m"。
		IdleSpeakInterval time.Duration `mapstructure:"idle_speak_interval"`
		// IdleSpeakPrompt 是主动发言时给模型的提示词，为空用内置兜底。
		IdleSpeakPrompt string `mapstructure:"idle_speak_prompt"`
	} `mapstructure:"agent"`
	TTS struct {
		// Engines 是引擎降级顺序，例如 ["edge_tts", "openai_tts"]；为空表示不启用语音播报。
		Engines []string `mapstructure:"engines"`

		Edge struct {
			Voice          string `mapstructure:"voice"`
			Rate           string `mapstructure:"rate"`
			Volume         string `mapstructure:"volume"`
			Pitch          string `mapstructure:"pitch"`
			Proxy          string `mapstructure:"proxy"`
			ReceiveTimeout int    `mapstructure:"receive_timeout"`
		} `mapstructure:"edge_tts"`

		OpenAI struct {
			BaseURL string  `mapstructure:"base_url"`
			APIKey  string  `mapstructure:"api_key"`
			Model   string  `mapstructure:"model"`
			Voice   string  `mapstructure:"voice"`
			Speed   float64 `mapstructure:"speed"`
		} `mapstructure:"openai_tts"`

		SiliconFlow struct {
			BaseURL    string  `mapstructure:"base_url"`
			APIKey     string  `mapstructure:"api_key"`
			Model      string  `mapstructure:"model"`
			Voice      string  `mapstructure:"voice"`
			Speed      float64 `mapstructure:"speed"`
			SampleRate int     `mapstructure:"sample_rate"`
			Gain       float64 `mapstructure:"gain"`
		} `mapstructure:"siliconflow_tts"`

		Fish struct {
			BaseURL     string `mapstructure:"base_url"`
			APIKey      string `mapstructure:"api_key"`
			Model       string `mapstructure:"model"`
			ReferenceID string `mapstructure:"reference_id"`
			Latency     string `mapstructure:"latency"`
			SampleRate  int    `mapstructure:"sample_rate"`
		} `mapstructure:"fish_api_tts"`

		Minimax struct {
			BaseURL    string `mapstructure:"base_url"`
			GroupID    string `mapstructure:"group_id"`
			APIKey     string `mapstructure:"api_key"`
			Model      string `mapstructure:"model"`
			VoiceID    string `mapstructure:"voice_id"`
			SampleRate int    `mapstructure:"sample_rate"`
		} `mapstructure:"minimax_tts"`
	} `mapstructure:"tts"`
	Broadcast struct {
		Concurrency int           `mapstructure:"concurrency"`  // 并行合成上限
		MaxPending  int           `mapstructure:"max_pending"`  // 待合成条目上限
		MinInterval time.Duration `mapstructure:"min_interval"` // 两次播报之间的冷却
	} `mapstructure:"broadcast"`
	Frontend struct {
		Enabled bool `mapstructure:"enabled"` // 显式开关；配了模型目录时自动启用
		// ModelsDir 是 Live2D 模型根目录，结构为 <模型名>/runtime/<模型名>.model3.json。
		ModelsDir string `mapstructure:"models_dir"`
		// ModelDict 是原项目的 model_dict.json，提供缩放、偏移与 emotionMap。
		ModelDict string `mapstructure:"model_dict"`
		// ModelScale 是模型显示倍率：1.0 = 正好占满视口高度；0 表示沿用模型清单里的值。
		ModelScale float64 `mapstructure:"model_scale"`
	} `mapstructure:"frontend"`
	Stream  StreamConfig   `mapstructure:"stream"`
	Clients []ClientConfig `mapstructure:"clients"`
}

type ClientConfig struct {
	Platform   string `mapstructure:"platform"`
	AdapterKey string `mapstructure:"adapter_key"`
	Path       string `mapstructure:"path"`
}

// StreamConfig 是推流配置：开播取地址 → 虚拟屏渲染 → ffmpeg 推 RTMP。
type StreamConfig struct {
	Enabled bool `mapstructure:"enabled"` // 显式开关；开了才开播与推流
	// Output 直接给完整推流地址（rtmp://...&key=...）时跳过自动开播，便于自检与手填。
	Output string `mapstructure:"output"`
	// Cookie 是 B 站登录态（至少含 SESSDATA 与 bili_jct），留空回退环境变量 BILIBILI_COOKIE。
	Cookie string `mapstructure:"cookie"`
	// RoomID 是直播间号；AreaID 留空表示沿用直播间当前分区。
	RoomID int64  `mapstructure:"room_id"`
	AreaID string `mapstructure:"area_id"`
	// Input 是画面来源：screen（抓虚拟屏，默认）或 test（ffmpeg 测试画面，自检用）。
	Input string `mapstructure:"input"`
	// Renderer 为真时由本进程起 Xvfb + Chrome 渲染 /web/ 页面；显示与浏览器自备时置 false。
	Renderer bool   `mapstructure:"renderer"`
	Display  string `mapstructure:"display"`
	Xvfb     string `mapstructure:"xvfb"`
	Chrome   string `mapstructure:"chrome"`
	FFmpeg   string `mapstructure:"ffmpeg"`
	// 尺寸需与虚拟屏一致，错开会缩放；FPS 与码率按上行带宽调。
	Width        int    `mapstructure:"width"`
	Height       int    `mapstructure:"height"`
	FPS          int    `mapstructure:"fps"`
	VideoBitrate string `mapstructure:"video_bitrate"`
	AudioBitrate string `mapstructure:"audio_bitrate"`
	Encoder      string `mapstructure:"encoder"` // 默认 libx264；有 N 卡可换 h264_nvenc
	// RestartWait 是 ffmpeg 异常退出后的重启间隔，0=默认 5s，<0 表示不重启。
	RestartWait time.Duration `mapstructure:"restart_wait"`
}

func ProvideConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(".")

	// 凭据不进仓库：对应的 api_key 留空时回退到环境变量
	envBindings := map[string]string{
		"llm.api_key":                 "LLM_API_KEY",
		"tts.openai_tts.api_key":      "OPENAI_API_KEY",
		"tts.siliconflow_tts.api_key": "SILICONFLOW_API_KEY",
		"tts.fish_api_tts.api_key":    "FISH_API_KEY",
		"tts.minimax_tts.api_key":     "MINIMAX_API_KEY",
		"stream.cookie":               "BILIBILI_COOKIE",
	}
	for key, env := range envBindings {
		if err := v.BindEnv(key, env); err != nil {
			return nil, fmt.Errorf("bind env %s: %w", env, err)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return &cfg, nil
}
