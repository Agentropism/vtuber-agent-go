package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCharacter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `
name = "测试角色"
avatar = "test.png"
live2d_model = "test_model"
system_prompt = """
你是测试角色。
"""
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入角色文件: %v", err)
	}

	got, err := LoadCharacter(path)
	if err != nil {
		t.Fatalf("加载角色: %v", err)
	}
	if got.Name != "测试角色" || got.Avatar != "test.png" || got.Live2DModel != "test_model" {
		t.Fatalf("角色字段解析错误: %#v", got)
	}
	if got.SystemPrompt != "你是测试角色。\n" {
		t.Fatalf("系统提示词 = %q", got.SystemPrompt)
	}
}

// 路径为空是合法用法：调用方回退到 [agent].system_prompt 的行内提示词。
func TestLoadCharacterEmptyPath(t *testing.T) {
	got, err := LoadCharacter("")
	if err != nil {
		t.Fatalf("空路径不应报错: %v", err)
	}
	if got != (Character{}) {
		t.Fatalf("空路径应返回零值: %#v", got)
	}
}

// 路径写错属于部署错误，必须报错而不是静默用兜底人格。
func TestLoadCharacterMissingFile(t *testing.T) {
	if _, err := LoadCharacter(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("文件不存在时应报错")
	}
}

// 仓库自带的角色资产必须能被解析，改坏了要及时发现。
func TestLoadBundledCharacter(t *testing.T) {
	// 相对本包目录（internal/config）回退两级到仓库根；包位置变动时必须同步这里。
	got, err := LoadCharacter(filepath.Join("..", "..", "characters", "mili.toml"))
	if err != nil {
		t.Fatalf("加载仓库角色资产: %v", err)
	}
	if got.Name == "" || got.Live2DModel == "" || got.SystemPrompt == "" {
		t.Fatalf("角色资产字段不完整: %#v", got)
	}
}
