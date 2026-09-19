package config

import (
	"fmt"
	"os"
	"strconv"
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
	Bilibili BilibiliConfig `mapstructure:"bilibili"`
	LLM      struct {
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
	Clients []ClientConfig `mapstructure:"clients"`
}

type ClientConfig struct {
	Platform   string `mapstructure:"platform"`
	AdapterKey string `mapstructure:"adapter_key"`
	Path       string `mapstructure:"path"`
}

// BilibiliConfig 是 B 站直播开放平台的接入配置。
type BilibiliConfig struct {
	Enabled bool `mapstructure:"enabled"` // 显式开关；凭据齐全时自动启用
	// Host 是开放平台 HTTP 地址，留空用官方地址 https://live-open.biliapi.com。
	Host string `mapstructure:"host"`
	// 凭据不进仓库：留空时回退到同名环境变量 BILIBILI_ACCESS_KEY 等。
	AccessKey         string        `mapstructure:"access_key"`
	AccessKeySecret   string        `mapstructure:"access_key_secret"`
	IDCode            string        `mapstructure:"id_code"` // 主播身份码
	AppID             int64         `mapstructure:"app_id"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"` // 心跳周期，0=默认 20s
}

// applyEnv 用环境变量补齐没有写在配置文件里的凭据。
//
// 这里手动读而不是走 viper.BindEnv：app_id 是整数，viper 的字符串绑定遇到
// 数字类型解码并不稳妥，统一手写反而更好预期。
func (b *BilibiliConfig) applyEnv() {
	if b.AccessKey == "" {
		b.AccessKey = os.Getenv("BILIBILI_ACCESS_KEY")
	}
	if b.AccessKeySecret == "" {
		b.AccessKeySecret = os.Getenv("BILIBILI_ACCESS_KEY_SECRET")
	}
	if b.IDCode == "" {
		b.IDCode = os.Getenv("BILIBILI_ID_CODE")
	}
	if b.AppID == 0 {
		if v := os.Getenv("BILIBILI_APP_ID"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				b.AppID = n
			}
		}
	}
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

	cfg.Bilibili.applyEnv()

	return &cfg, nil
}
