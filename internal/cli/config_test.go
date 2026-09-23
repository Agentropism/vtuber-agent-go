package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// templateContent 是一份最小的模板：init 只做复制，内容原样搬过去。
const templateContent = "[server]\naddr = \":6199\"\n\n[llm]\napi_key = \"sk-from-template\"\n"

// prepareInitDir 造一个「当前工作目录里有模板」的场景。
func prepareInitDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, templateFileName), []byte(templateContent), 0o644); err != nil {
		t.Fatalf("写模板失败: %v", err)
	}
	t.Chdir(dir)

	return dir
}

func TestConfigInitCopiesTemplate(t *testing.T) {
	dir := prepareInitDir(t)

	out, code := runCLI(t, "config", "init")
	if code != 0 {
		t.Fatalf("退出码 = %d, want 0\n%s", code, out)
	}

	got, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("没有生成配置文件: %v", err)
	}
	if string(got) != templateContent {
		t.Errorf("生成内容与模板不一致:\n%s", got)
	}
}

// 生成的文件里会填 API key 与登录态，权限必须是 0600。
func TestConfigInitUsesPrivatePermissions(t *testing.T) {
	dir := prepareInitDir(t)

	if _, code := runCLI(t, "config", "init"); code != 0 {
		t.Fatalf("退出码 = %d, want 0", code)
	}

	info, err := os.Stat(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("权限 = %o, want 600", perm)
	}
}

// 默认拒绝覆盖：目标里可能已经有真凭据，手滑重跑一次就洗掉了。
func TestConfigInitRefusesExistingWithoutForce(t *testing.T) {
	dir := prepareInitDir(t)
	target := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(target, []byte("api_key = \"real-key\"\n"), 0o600); err != nil {
		t.Fatalf("写已有配置失败: %v", err)
	}

	out, code := runCLI(t, "config", "init")
	if code == 0 {
		t.Fatal("已存在时应以非零退出")
	}
	if !strings.Contains(out, "--force") {
		t.Errorf("错误信息应提示 --force，实际:\n%s", out)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if string(got) != "api_key = \"real-key\"\n" {
		t.Errorf("没有 --force 时不该改动已有文件，实际:\n%s", got)
	}
}

func TestConfigInitForceOverwrites(t *testing.T) {
	dir := prepareInitDir(t)
	target := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(target, []byte("旧内容\n"), 0o600); err != nil {
		t.Fatalf("写已有配置失败: %v", err)
	}

	if out, code := runCLI(t, "config", "init", "--force"); code != 0 {
		t.Fatalf("退出码 = %d, want 0\n%s", code, out)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if string(got) != templateContent {
		t.Errorf("--force 应覆盖成模板内容，实际:\n%s", got)
	}
}

// 模板不存在时要报出「去哪里拿」，而不是只说读文件失败。
func TestConfigInitWithoutTemplateExplainsSource(t *testing.T) {
	t.Chdir(t.TempDir())

	out, code := runCLI(t, "config", "init")
	if code == 0 {
		t.Fatal("没有模板时应以非零退出")
	}
	if !strings.Contains(out, templateFileName) || !strings.Contains(out, "工作目录") {
		t.Errorf("错误信息应说明模板来源，实际:\n%s", out)
	}
}

func TestConfigInitHonorsConfigFlag(t *testing.T) {
	dir := prepareInitDir(t)
	target := filepath.Join(dir, "custom.toml")

	out, code := runCLI(t, "config", "init", "--config", target, "--json")
	if code != 0 {
		t.Fatalf("退出码 = %d, want 0\n%s", code, out)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout 不是 JSON: %v\n%s", err, out)
	}
	if result["path"] != target || result["source"] != templateFileName || result["created"] != true {
		t.Errorf("--json 字段不符合契约: %#v", result)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("没有写到 --config 指定的路径: %v", err)
	}
}

// writeShowConfig 造一份带凭据的配置，用来验证 show 的脱敏。
func writeShowConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[llm]\napi_key = \"sk-super-secret\"\nbase_url = \"https://api.example.com/v1\"\n\n" +
		"[stream]\ncookie = \"SESSDATA=secret\"\ncookie_file = \"data/bilibili-cookie.txt\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}

	return path
}

// show --json 必须与 GET /api/config 同构：这里钉住脱敏形状与「不回明文」。
func TestConfigShowJSONRedactsSecrets(t *testing.T) {
	path := writeShowConfig(t)

	out, code := runCLI(t, "config", "show", "--config", path, "--json")
	if code != 0 {
		t.Fatalf("退出码 = %d, want 0\n%s", code, out)
	}
	if strings.Contains(out, "sk-super-secret") || strings.Contains(out, "SESSDATA=secret") {
		t.Fatalf("--json 泄漏了明文凭据:\n%s", out)
	}

	var snapshot map[string]any
	if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
		t.Fatalf("stdout 不是 JSON: %v\n%s", err, out)
	}

	llm, ok := snapshot["llm"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 llm 段: %#v", snapshot["llm"])
	}
	apiKey, ok := llm["api_key"].(map[string]any)
	if !ok || apiKey["configured"] != true {
		t.Errorf("api_key 应回 {\"configured\": true}，实际 %#v", llm["api_key"])
	}

	stream, ok := snapshot["stream"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 stream 段: %#v", snapshot["stream"])
	}
	cookie, ok := stream["cookie"].(map[string]any)
	if !ok || cookie["configured"] != true {
		t.Errorf("cookie 应回 {\"configured\": true}，实际 %#v", stream["cookie"])
	}
	// cookie_file 是路径不是凭据，要照常回值。
	if stream["cookie_file"] != "data/bilibili-cookie.txt" {
		t.Errorf("cookie_file 应原样回值，实际 %#v", stream["cookie_file"])
	}
}

func TestConfigShowTOMLMode(t *testing.T) {
	path := writeShowConfig(t)

	out, code := runCLI(t, "config", "show", "--config", path)
	if code != 0 {
		t.Fatalf("退出码 = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "[llm]") || !strings.Contains(out, "configured = true") {
		t.Errorf("人读模式应输出 TOML 形式的脱敏快照，实际:\n%s", out)
	}
	if strings.Contains(out, "sk-super-secret") {
		t.Errorf("人读模式同样不能回明文:\n%s", out)
	}
}

func TestConfigShowMissingFileFails(t *testing.T) {
	out, code := runCLI(t, "config", "show", "--config", filepath.Join(t.TempDir(), "nope.toml"))
	if code == 0 {
		t.Fatal("配置不存在时应以非零退出")
	}
	if strings.Contains(out, "configured") {
		t.Errorf("失败时不该输出配置快照:\n%s", out)
	}
}
