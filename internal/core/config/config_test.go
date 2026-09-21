package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeConfig 在临时目录写入 config.toml 并把工作目录切过去。
func writeConfig(t *testing.T, content string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}
	t.Chdir(dir)
}

func TestProvideConfigReadsLLMSection(t *testing.T) {
	writeConfig(t, `
[llm]
base_url = "https://api.deepseek.com/v1"
api_key = ""
model = "deepseek-chat"
temperature = 0.7
`)

	cfg, err := ProvideConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}

	if cfg.LLM.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("base_url 不符合预期: %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != "deepseek-chat" {
		t.Errorf("model 不符合预期: %q", cfg.LLM.Model)
	}
	if cfg.LLM.Temperature != 0.7 {
		t.Errorf("temperature 不符合预期: %v", cfg.LLM.Temperature)
	}
}

func TestProvideConfigLLMAPIKeyFromEnv(t *testing.T) {
	writeConfig(t, `
[llm]
base_url = "https://api.deepseek.com/v1"
api_key = ""
model = "deepseek-chat"
`)

	t.Setenv("LLM_API_KEY", "from-env")

	cfg, err := ProvideConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if cfg.LLM.APIKey != "from-env" {
		t.Errorf("api_key 未回退到环境变量，实际 %q", cfg.LLM.APIKey)
	}
}

func TestProvideConfigLLMAPIKeyFromFile(t *testing.T) {
	writeConfig(t, `
[llm]
base_url = "https://api.deepseek.com/v1"
api_key = "from-file"
model = "deepseek-chat"
`)

	cfg, err := ProvideConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if cfg.LLM.APIKey != "from-file" {
		t.Errorf("api_key 应取自配置文件，实际 %q", cfg.LLM.APIKey)
	}
}

func TestProvideConfigMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())

	if _, err := ProvideConfig(); err == nil {
		t.Error("配置文件缺失时应返回错误")
	}
}

// 时间段写作 "600s" / "5m"，解析走 Duration.UnmarshalText——go-toml 不像 viper 自带
// 这个转换。这条是 viper → go-toml 迁移的回归锁：坏了会在启动时就报「时间段解析失败」。
func TestProvideConfigParsesDurations(t *testing.T) {
	writeConfig(t, `
[memory]
queue_wait_timeout = "90s"
dedup_ttl = "600s"

[agent]
turn_timeout = "60s"
idle_speak_interval = "5m"

[broadcast]
min_interval = "3s"

[stream]
restart_wait = "5s"
`)

	cfg, err := ProvideConfig()
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}

	got := map[string]time.Duration{
		"memory.queue_wait_timeout": cfg.Memory.QueueWaitTimeout.Std(),
		"memory.dedup_ttl":          cfg.Memory.DedupTTL.Std(),
		"agent.turn_timeout":        cfg.Agent.TurnTimeout.Std(),
		"agent.idle_speak_interval": cfg.Agent.IdleSpeakInterval.Std(),
		"broadcast.min_interval":    cfg.Broadcast.MinInterval.Std(),
		"stream.restart_wait":       cfg.Stream.RestartWait.Std(),
	}
	want := map[string]time.Duration{
		"memory.queue_wait_timeout": 90 * time.Second,
		"memory.dedup_ttl":          600 * time.Second,
		"agent.turn_timeout":        60 * time.Second,
		"agent.idle_speak_interval": 5 * time.Minute,
		"broadcast.min_interval":    3 * time.Second,
		"stream.restart_wait":       5 * time.Second,
	}

	for key, value := range got {
		if value != want[key] {
			t.Errorf("%s = %v, want %v", key, value, want[key])
		}
	}
}

// 写错的时间段必须报错：0 在几处表示「不限」，静默降级成 0 很危险。
func TestProvideConfigRejectsBadDuration(t *testing.T) {
	writeConfig(t, `
[agent]
turn_timeout = "60 秒"
`)

	if _, err := ProvideConfig(); err == nil {
		t.Fatal("非法时间段应当报错")
	}
}
