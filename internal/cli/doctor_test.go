package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeDoctorConfig 写一份最小可用配置，返回路径。
func writeDoctorConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}

	return path
}

const minimalConfig = `
[server]
addr = "127.0.0.1:0"

[llm]
base_url = "https://api.example.com/v1"
api_key = "sk-test"
model = "test-model"
`

// 检查项名与顺序是契约：与 docs/CLI.md 的表格一一对应，只允许往后追加。
func TestDoctorReportContract(t *testing.T) {
	path := writeDoctorConfig(t, minimalConfig)
	report := runDoctor(path)

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	for _, key := range []string{"ok", "config", "checks", "summary"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("JSON 里缺少契约字段 %q（现有: %v）", key, keysOf(decoded))
		}
	}

	names := make([]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		names = append(names, check.Name)

		switch check.Status {
		case statusPass, statusWarn, statusFail, statusSkip:
		default:
			t.Errorf("检查项 %s 的状态 %q 不在契约取值里", check.Name, check.Status)
		}
		if check.Message == "" {
			t.Errorf("检查项 %s 没有消息：契约要求 message 非空", check.Name)
		}
	}

	if !slices.Equal(names, doctorCheckNames) {
		t.Errorf("检查项顺序 = %v, want %v", names, doctorCheckNames)
	}
}

func TestDoctorSummaryMatchesChecks(t *testing.T) {
	path := writeDoctorConfig(t, minimalConfig)
	report := runDoctor(path)

	want := summarize(report.Checks)
	if report.Summary != want {
		t.Errorf("summary = %+v, want %+v", report.Summary, want)
	}

	total := report.Summary.Pass + report.Summary.Warn + report.Summary.Fail + report.Summary.Skip
	if total != len(report.Checks) {
		t.Errorf("summary 合计 %d != 检查项数 %d", total, len(report.Checks))
	}
}

func TestDoctorMinimalConfigPasses(t *testing.T) {
	path := writeDoctorConfig(t, minimalConfig)
	report := runDoctor(path)

	if !report.OK {
		t.Fatalf("最小可用配置应当通过，实际: %+v", report)
	}
	if report.Summary.Fail != 0 {
		t.Errorf("不该有失败项: %+v", report)
	}
}

// 配置文件读不出来时，只有第一项失败，其余明确标 skip——不拿零值猜。
func TestDoctorMissingConfigOnlyConfigCheckFails(t *testing.T) {
	report := runDoctor(filepath.Join(t.TempDir(), "nope.toml"))

	if report.OK {
		t.Error("配置不可用时 ok 应为 false")
	}
	if report.Checks[0].Name != "config" || report.Checks[0].Status != statusFail {
		t.Errorf("第一项应是 config 的 fail，实际 %+v", report.Checks[0])
	}
	for _, check := range report.Checks[1:] {
		if check.Status != statusSkip {
			t.Errorf("%s 应是 skip，实际 %q", check.Name, check.Status)
		}
	}
}

// 警告不影响 ok 与退出码：开播前扫码补登录态是正常流程，不该把门禁弄红。
func TestDoctorWarnKeepsOK(t *testing.T) {
	path := writeDoctorConfig(t, minimalConfig+`
[stream]
enabled = true
room_id = 123456
`)
	report := runDoctor(path)

	if report.Summary.Warn == 0 {
		t.Fatalf("缺登录态应产生警告，实际 %+v", report.Summary)
	}
	if !report.OK {
		t.Errorf("只有警告时 ok 应为 true，实际 %+v", report)
	}
	if hasFailure(report.Checks) {
		t.Error("hasFailure 不该把警告算成失败")
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	return keys
}
