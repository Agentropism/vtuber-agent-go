package config

import (
	"fmt"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Log struct {
		Level string `toml:"level"`
	} `toml:"log"`
	Memory struct {
		QueueSize          int      `toml:"queue_size"`           // 上传缓存容量，默认 256
		QueueWaitTimeout   Duration `toml:"queue_wait_timeout"`   // 缓存满时等待上限，0=无限等待
		SensitiveWordsFile string   `toml:"sensitive_words_file"` // 敏感词库文件路径，空=不启用
		DedupTTL           Duration `toml:"dedup_ttl"`            // 去重窗口，0=不启用
	} `toml:"memory"`
	Server struct {
		Addr string `toml:"addr"`
	} `toml:"server"`
	LLM struct {
		BaseURL     string  `toml:"base_url"`    // OpenAI 兼容端点，例如 https://api.deepseek.com/v1
		APIKey      string  `toml:"api_key"`     // 优先取环境变量 LLM_API_KEY
		Model       string  `toml:"model"`       // 例如 deepseek-chat
		Temperature float64 `toml:"temperature"` // 0 表示使用默认值 1.0
	} `toml:"llm"`
	Agent struct {
		SystemPrompt  string   `toml:"system_prompt"`   // 角色系统提示词，为空时用内置兜底值
		CharacterFile string   `toml:"character_file"`  // 角色定义文件（TOML），非空时其 system_prompt 优先
		MaxToolRounds int      `toml:"max_tool_rounds"` // 单轮对话内工具循环上限，0=默认 8
		InterruptMode string   `toml:"interrupt_mode"`  // 打断标记角色：user（默认）/ system
		QueueSize     int      `toml:"queue_size"`      // 每个会话的待处理消息上限
		TurnTimeout   Duration `toml:"turn_timeout"`    // 单轮对话超时，例如 "60s"

		// 历史裁剪：两者同时生效，0 表示用默认值（12 轮 / 4000 token）。
		HistoryMaxTurns  int `toml:"history_max_turns"`
		HistoryMaxTokens int `toml:"history_max_tokens"`

		// MemoryFile 是长期记忆文件（JSON Lines），为空表示不启用。
		MemoryFile string `toml:"memory_file"`
		// RecallLimit 是每轮召回的历史条数，0 表示用默认值 5。
		RecallLimit int `toml:"recall_limit"`

		// ArchiveDir 是对话归档目录（JSON Lines，按平台/渠道分类），为空表示不启用。
		ArchiveDir string `toml:"archive_dir"`
		// HistoryRehydrateTurns 是渠道首次创建时从归档恢复的对话轮数，0 表示不恢复。
		HistoryRehydrateTurns int `toml:"history_rehydrate_turns"`

		// EnableTools 打开工具调用（记忆检索、状态查询）。
		EnableTools bool `toml:"enable_tools"`
		// IdleSpeakInterval > 0 时，渠道静默超过该时长就主动说一句，例如 "5m"。
		IdleSpeakInterval Duration `toml:"idle_speak_interval"`
		// IdleSpeakPrompt 是主动发言时给模型的提示词，为空用内置兜底。
		IdleSpeakPrompt string `toml:"idle_speak_prompt"`
	} `toml:"agent"`
	TTS struct {
		// Engines 是引擎降级顺序，例如 ["edge_tts", "openai_tts"]；为空表示不启用语音播报。
		Engines []string `toml:"engines"`

		Edge struct {
			Voice          string `toml:"voice"`
			Rate           string `toml:"rate"`
			Volume         string `toml:"volume"`
			Pitch          string `toml:"pitch"`
			Proxy          string `toml:"proxy"`
			ReceiveTimeout int    `toml:"receive_timeout"`
		} `toml:"edge_tts"`

		OpenAI struct {
			BaseURL string  `toml:"base_url"`
			APIKey  string  `toml:"api_key"`
			Model   string  `toml:"model"`
			Voice   string  `toml:"voice"`
			Speed   float64 `toml:"speed"`
		} `toml:"openai_tts"`

		SiliconFlow struct {
			BaseURL    string  `toml:"base_url"`
			APIKey     string  `toml:"api_key"`
			Model      string  `toml:"model"`
			Voice      string  `toml:"voice"`
			Speed      float64 `toml:"speed"`
			SampleRate int     `toml:"sample_rate"`
			Gain       float64 `toml:"gain"`
		} `toml:"siliconflow_tts"`

		Fish struct {
			BaseURL     string `toml:"base_url"`
			APIKey      string `toml:"api_key"`
			Model       string `toml:"model"`
			ReferenceID string `toml:"reference_id"`
			Latency     string `toml:"latency"`
			SampleRate  int    `toml:"sample_rate"`
		} `toml:"fish_api_tts"`

		Minimax struct {
			BaseURL    string `toml:"base_url"`
			GroupID    string `toml:"group_id"`
			APIKey     string `toml:"api_key"`
			Model      string `toml:"model"`
			VoiceID    string `toml:"voice_id"`
			SampleRate int    `toml:"sample_rate"`
		} `toml:"minimax_tts"`
	} `toml:"tts"`
	Broadcast struct {
		Concurrency int      `toml:"concurrency"`  // 并行合成上限
		MaxPending  int      `toml:"max_pending"`  // 待合成条目上限
		MinInterval Duration `toml:"min_interval"` // 两次播报之间的冷却
	} `toml:"broadcast"`
	Frontend struct {
		Enabled bool `toml:"enabled"` // 显式开关；配了模型目录时自动启用
		// ModelsDir 是 Live2D 模型根目录，结构为 <模型名>/runtime/<模型名>.model3.json。
		ModelsDir string `toml:"models_dir"`
		// ModelDict 是原项目的 model_dict.json，提供缩放、偏移与 emotionMap。
		ModelDict string `toml:"model_dict"`
		// ModelScale 是模型显示倍率：1.0 = 正好占满视口高度；0 表示沿用模型清单里的值。
		ModelScale float64 `toml:"model_scale"`
	} `toml:"frontend"`
	Stream  StreamConfig   `toml:"stream"`
	Clients []ClientConfig `toml:"clients"`
}

type ClientConfig struct {
	Platform   string `toml:"platform"`
	AdapterKey string `toml:"adapter_key"`
	Path       string `toml:"path"`
}

// StreamConfig 是推流配置：开播取地址 → 虚拟屏渲染 → ffmpeg 推 RTMP。
type StreamConfig struct {
	Enabled bool `toml:"enabled"` // 显式开关；开了才开播与推流
	// Output 直接给完整推流地址（rtmp://...&key=...）时跳过自动开播，便于自检与手填。
	Output string `toml:"output"`
	// Cookie 是 B 站登录态（至少含 SESSDATA 与 bili_jct），留空回退环境变量 BILIBILI_COOKIE。
	Cookie string `toml:"cookie"`
	// RoomID 是直播间号；AreaID 留空表示沿用直播间当前分区。
	RoomID int64  `toml:"room_id"`
	AreaID string `toml:"area_id"`
	// CookieFile 是扫码登录（/login/）落盘的登录态路径，留空用默认值。
	CookieFile string `toml:"cookie_file"`
	// Input 是画面来源：screen（抓虚拟屏，默认）或 test（ffmpeg 测试画面，自检用）。
	Input string `toml:"input"`
	// Renderer 为真时由本进程起 Xvfb + Chrome 渲染首页；显示与浏览器自备时置 false。
	Renderer bool   `toml:"renderer"`
	Display  string `toml:"display"`
	Xvfb     string `toml:"xvfb"`
	Chrome   string `toml:"chrome"`
	FFmpeg   string `toml:"ffmpeg"`
	// 尺寸需与虚拟屏一致，错开会缩放；FPS 与码率按上行带宽调。
	Width        int    `toml:"width"`
	Height       int    `toml:"height"`
	FPS          int    `toml:"fps"`
	VideoBitrate string `toml:"video_bitrate"`
	AudioBitrate string `toml:"audio_bitrate"`
	Encoder      string `toml:"encoder"` // 默认 libx264；有 N 卡可换 h264_nvenc
	// RestartWait 是 ffmpeg 异常退出后的重启间隔，0=默认 5s，<0 表示不重启。
	RestartWait Duration `toml:"restart_wait"`
}

// configFileName 是配置文件名：从工作目录读取的 config.toml。
const configFileName = "config.toml"

// ProvideConfig 每次从磁盘重读 config.toml（允许运行时改配置）。
func ProvideConfig() (*Config, error) {
	data, err := os.ReadFile(configFileName)
	if err != nil {
		return nil, fmt.Errorf("读配置 %s: %w", configFileName, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %s: %w", configFileName, err)
	}

	cfg.applyEnv()

	return &cfg, nil
}

// applyEnv 用环境变量补齐没有写在配置文件里的凭据（凭据不进仓库）。
func (c *Config) applyEnv() {
	fill := func(dst *string, env string) {
		if *dst == "" {
			*dst = os.Getenv(env)
		}
	}

	fill(&c.LLM.APIKey, "LLM_API_KEY")
	fill(&c.TTS.OpenAI.APIKey, "OPENAI_API_KEY")
	fill(&c.TTS.SiliconFlow.APIKey, "SILICONFLOW_API_KEY")
	fill(&c.TTS.Fish.APIKey, "FISH_API_KEY")
	fill(&c.TTS.Minimax.APIKey, "MINIMAX_API_KEY")
	fill(&c.Stream.Cookie, "BILIBILI_COOKIE")
}

// Duration 让 TOML 里的 "600s" / "5m" 这类写法能直接解码成时间段。
//
// go-toml 不像 viper 那样自带 duration 解析，而配置里全是这种写法，
// 所以给 time.Duration 包一层实现 TextUnmarshaler；取用时用 Std() 转回。
type Duration time.Duration

// Std 转回标准库类型。
func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("时间段 %q 解析失败: %w", text, err)
	}

	*d = Duration(parsed)

	return nil
}
