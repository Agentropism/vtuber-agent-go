package cli

import (
	"encoding/json"
	"runtime"
	"testing"
)

// setVersion 临时替换版本变量，返回恢复函数。
func setVersion(t *testing.T, version, commit, buildTime string) {
	t.Helper()

	oldVersion, oldCommit, oldBuildTime := Version, Commit, BuildTime
	Version, Commit, BuildTime = version, commit, buildTime
	t.Cleanup(func() { Version, Commit, BuildTime = oldVersion, oldCommit, oldBuildTime })
}

// --json 的四个字段是契约（docs/CLI.md），字段名与个数都要钉住。
func TestVersionJSONFields(t *testing.T) {
	setVersion(t, "1.2.3", "abc1234", "2026-09-23T12:00:00+08:00")

	out, code := runCLI(t, "version", "--json")
	if code != 0 {
		t.Fatalf("退出码 = %d, want 0\n%s", code, out)
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("stdout 不是 JSON: %v\n%s", err, out)
	}

	if len(info) != 4 {
		t.Errorf("字段数 = %d, want 4: %#v", len(info), info)
	}
	for key, want := range map[string]string{
		"version":    "1.2.3",
		"commit":     "abc1234",
		"build_time": "2026-09-23T12:00:00+08:00",
		"go_version": runtime.Version(),
	} {
		if info[key] != want {
			t.Errorf("%s = %v, want %q", key, info[key], want)
		}
	}
}

// 没注入时必须是空字符串：不造假值，也不把它写成 "unknown" 之类的伪值。
func TestVersionJSONKeepsEmptyFieldsEmpty(t *testing.T) {
	setVersion(t, "dev", "", "")

	out, code := runCLI(t, "version", "--json")
	if code != 0 {
		t.Fatalf("退出码 = %d, want 0", code)
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("stdout 不是 JSON: %v", err)
	}
	if info["commit"] != "" || info["build_time"] != "" {
		t.Errorf("未注入的字段应为空串，实际 %#v", info)
	}
}

// 人读模式把未注入的字段显示成「未知」，而不是留空让人以为坏了。
func TestVersionTextShowsUnknownForEmptyFields(t *testing.T) {
	setVersion(t, "dev", "", "")

	out, code := runCLI(t, "version")
	if code != 0 {
		t.Fatalf("退出码 = %d, want 0", code)
	}
	for _, want := range []string{"版本", "dev", "commit", "构建时间", "未知", "Go"} {
		if !contains(out, want) {
			t.Errorf("人读输出缺少 %q:\n%s", want, out)
		}
	}
}

func TestVersionRejectsArgs(t *testing.T) {
	if _, code := runCLI(t, "version", "多余参数"); code == 0 {
		t.Error("多余的参数应被拒绝")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}

	return -1
}
