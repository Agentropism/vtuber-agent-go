package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// checkStatus 是检查结果的取值。这是对外契约（docs/CLI.md），改名算破坏性变更。
type checkStatus string

const (
	statusPass checkStatus = "pass"
	statusWarn checkStatus = "warn"
	statusFail checkStatus = "fail"
	statusSkip checkStatus = "skip"
)

// checkResult 是一项检查的结果。Name / Status / Message 是契约字段；
// Details 只为人读排障服务，内部字段不承诺稳定。
type checkResult struct {
	Name    string         `json:"name"`
	Status  checkStatus    `json:"status"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// summaryCounts 是各状态的计数。
type summaryCounts struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

func newCheck(name string, status checkStatus, message string) checkResult {
	return checkResult{Name: name, Status: status, Message: message}
}

func summarize(checks []checkResult) summaryCounts {
	var counts summaryCounts
	for _, check := range checks {
		switch check.Status {
		case statusPass:
			counts.Pass++
		case statusWarn:
			counts.Warn++
		case statusFail:
			counts.Fail++
		case statusSkip:
			counts.Skip++
		}
	}

	return counts
}

// hasFailure 只看 fail：warn 与 skip 都不算失败，退出码由它决定。
func hasFailure(checks []checkResult) bool {
	for _, check := range checks {
		if check.Status == statusFail {
			return true
		}
	}

	return false
}

// printer 统一两种出口：人读文本（可带颜色）与 JSON（纯数据，不带任何提示语）。
type printer struct {
	json  bool
	color bool
	out   io.Writer
}

func newPrinter(cmd *cobra.Command, opts *globalOptions) *printer {
	out := cmd.OutOrStdout()

	return &printer{
		json:  opts.json,
		color: !opts.json && isTerminal(out) && os.Getenv("NO_COLOR") == "",
		out:   out,
	}
}

// writeJSON 写出结构化结果。编码方式与后端接口一致（api.writeJSON），
// 以保证 config show --json 与 GET /api/config 的输出同构。
func (p *printer) writeJSON(body any) error {
	return json.NewEncoder(p.out).Encode(body)
}

// printChecks 打印检查清单：状态字形 + 契约名 + 说明。
func (p *printer) printChecks(checks []checkResult) {
	for _, check := range checks {
		fmt.Fprintf(p.out, "%s %-11s %s\n", p.paint(check.Status, statusGlyph(check.Status)), check.Name, check.Message)
	}
}

func (p *printer) printSummary(counts summaryCounts) {
	fmt.Fprintf(p.out, "\n通过 %d，警告 %d，失败 %d，跳过 %d\n", counts.Pass, counts.Warn, counts.Fail, counts.Skip)
}

// statusGlyph 是各状态的字形。skip 用中点而不是叉：它表示「没启用所以没查」。
func statusGlyph(status checkStatus) string {
	switch status {
	case statusPass:
		return "✔"
	case statusWarn:
		return "⚠"
	case statusFail:
		return "✖"
	default:
		return "·"
	}
}

// paint 按状态上色：通过绿、警告黄、失败红、跳过暗——与 TUI 主题的三态语义一致。
func (p *printer) paint(status checkStatus, text string) string {
	if !p.color {
		return text
	}

	code := map[checkStatus]string{
		statusPass: "32",
		statusWarn: "33",
		statusFail: "31",
		statusSkip: "2",
	}[status]

	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// isTerminal 判断输出是不是真终端：管道与重定向下不开颜色。
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}
