package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI 跑一次真实命令行（不经过 os.Exit），返回合并后的输出与退出码。
func runCLI(t *testing.T, args ...string) (string, int) {
	t.Helper()

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	code := execute(cmd, &out)

	return out.String(), code
}

// 裸跑不启动服务：只打帮助并以 1 退出（契约见 docs/CLI.md）。
func TestBareRunPrintsHelpAndFails(t *testing.T) {
	out, code := runCLI(t)

	if code != exitFailure {
		t.Errorf("退出码 = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(out, "可用子命令") || !strings.Contains(out, "run") {
		t.Errorf("裸跑应打印帮助并列出子命令，实际:\n%s", out)
	}
}

// 只给到父命令时与裸跑一致：帮助 + 非零退出码。
func TestParentCommandWithoutSubcommandFails(t *testing.T) {
	out, code := runCLI(t, "config")

	if code != exitFailure {
		t.Errorf("退出码 = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(out, "init") || !strings.Contains(out, "show") {
		t.Errorf("config 应列出子命令，实际:\n%s", out)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	out, code := runCLI(t, "nope")

	if code != exitFailure {
		t.Errorf("退出码 = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(out, "nope") {
		t.Errorf("错误信息应指出未知的命令名，实际:\n%s", out)
	}
}

// --help 是「用户明确要看帮助」，属于成功路径。
func TestExplicitHelpSucceeds(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"doctor", "--help"}} {
		out, code := runCLI(t, args...)
		if code != 0 {
			t.Errorf("%v 退出码 = %d, want 0", args, code)
		}
		if !strings.Contains(out, "用法") {
			t.Errorf("%v 应打印中文帮助，实际:\n%s", args, out)
		}
	}
}

// 命令表是契约的一部分：改名或加删除都必须同批改 docs/CLI.md。
func TestCommandSetMatchesContract(t *testing.T) {
	root := NewRootCommand()

	var names []string
	for _, sub := range root.Commands() {
		names = append(names, sub.Name())
	}

	want := []string{"config", "doctor", "help", "run", "version"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("命令表 = %v, want %v", names, want)
	}
}

// cobra 默认会挂 completion：已隐藏，不该出现在命令表里。
func TestCompletionIsHidden(t *testing.T) {
	out, _ := runCLI(t)

	if strings.Contains(out, "completion") {
		t.Errorf("completion 子命令应被隐藏，实际:\n%s", out)
	}
}

// 全局参数必须是 persistent：子命令也得认。
func TestGlobalFlagsReachSubcommands(t *testing.T) {
	root := NewRootCommand()
	for _, sub := range root.Commands() {
		if sub.Name() != "doctor" {
			continue
		}
		// persistent 参数在子命令上是「继承」来的，不在自己的 FlagSet 里。
		for _, flag := range []string{"config", "json"} {
			if sub.InheritedFlags().Lookup(flag) == nil {
				t.Errorf("doctor 上没有继承到 --%s", flag)
			}
		}
	}
}

// doctor --json 失败时也必须是「只有 JSON」：脚本靠 stdout 直接解析。
func TestDoctorJSONStaysPureOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// 故意不配 llm：必然有 fail 项
	if err := os.WriteFile(path, []byte("[server]\naddr = \"127.0.0.1:0\"\n"), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}

	out, code := runCLI(t, "doctor", "--config", path, "--json")
	if code != exitFailure {
		t.Errorf("退出码 = %d, want %d", code, exitFailure)
	}

	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("stdout 不是纯 JSON: %v\n输出:\n%s", err, out)
	}
	if report["ok"] != false {
		t.Errorf("有失败项时 ok 应为 false，实际 %v", report["ok"])
	}
	if report["config"] != path {
		t.Errorf("report.config = %v, want %s", report["config"], path)
	}
}
