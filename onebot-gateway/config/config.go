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
		Target             string        `mapstructure:"target"`
		CallbackPlatform   string        `mapstructure:"callback_platform"`
		QueueSize          int           `mapstructure:"queue_size"`           // 上传缓存容量，默认 256
		QueueWaitTimeout   time.Duration `mapstructure:"queue_wait_timeout"`   // 缓存满时等待上限，0=无限等待
		SensitiveWordsFile string        `mapstructure:"sensitive_words_file"` // 敏感词库文件路径，空=不启用
		DedupTTL           time.Duration `mapstructure:"dedup_ttl"`            // 去重窗口，0=不启用
	} `mapstructure:"memory"`
	Bilibili struct {
		DistilleryTarget string `mapstructure:"distillery_target"`
	} `mapstructure:"bilibili"`
	Server struct {
		Addr string `mapstructure:"addr"`
	} `mapstructure:"server"`
	LLM struct {
		BaseURL     string  `mapstructure:"base_url"`    // OpenAI 兼容端点，例如 https://api.deepseek.com/v1
		APIKey      string  `mapstructure:"api_key"`     // 优先取环境变量 LLM_API_KEY
		Model       string  `mapstructure:"model"`       // 例如 deepseek-chat
		Temperature float64 `mapstructure:"temperature"` // 0 表示使用默认值 1.0
	} `mapstructure:"llm"`
	Clients []ClientConfig `mapstructure:"clients"`
}

type ClientConfig struct {
	Platform   string `mapstructure:"platform"`
	AdapterKey string `mapstructure:"adapter_key"`
	Path       string `mapstructure:"path"`
}

func ProvideConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(".")

	// 凭据不进仓库：api_key 留空时回退到环境变量
	if err := v.BindEnv("llm.api_key", "LLM_API_KEY"); err != nil {
		return nil, fmt.Errorf("bind env: %w", err)
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
