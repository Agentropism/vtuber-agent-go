package config

import (
	"os"
	"path/filepath"
	"testing"
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
