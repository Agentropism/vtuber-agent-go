package config

import (
	"os"
	"path/filepath"
	"strings"
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

// --config 走 ProvideConfigFrom：路径要真的被用上，同时相对路径的基准仍是工作目录
// （配置里写的 characters/、data/ 不会因为配置文件在别处而改变含义）。
func TestProvideConfigFromExplicitPath(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()

	configPath := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(configPath, []byte(`
[llm]
base_url = "https://api.example.com/v1"
model = "custom-model"

[agent]
character_file = "characters/mili.toml"
`), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}

	t.Chdir(work)

	cfg, err := ProvideConfigFrom(configPath)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if cfg.LLM.Model != "custom-model" {
		t.Errorf("没有读到指定路径的配置: %q", cfg.LLM.Model)
	}
	// 相对路径按原样保留，由调用方以工作目录为基准解析。
	if cfg.Agent.CharacterFile != "characters/mili.toml" {
		t.Errorf("相对路径不应被改写: %q", cfg.Agent.CharacterFile)
	}
}

// 空路径回退到默认的 config.toml：显式传空与不传参行为一致。
func TestProvideConfigFromEmptyPathFallsBackToDefault(t *testing.T) {
	writeConfig(t, `
[llm]
model = "default-path-model"
`)

	cfg, err := ProvideConfigFrom("")
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if cfg.LLM.Model != "default-path-model" {
		t.Errorf("空路径应回退到 ./config.toml，实际 %q", cfg.LLM.Model)
	}
}

// 路径不存在时要报出是哪个路径，而不是只说「读配置失败」。
func TestProvideConfigFromMissingFileNamesThePath(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := ProvideConfigFrom("nope/missing.toml")
	if err == nil {
		t.Fatal("路径不存在时应返回错误")
	}
	if !strings.Contains(err.Error(), "nope/missing.toml") {
		t.Errorf("错误信息应包含路径，实际 %v", err)
	}
}
