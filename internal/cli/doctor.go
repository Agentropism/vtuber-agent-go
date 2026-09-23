package cli

import (
	"github.com/spf13/cobra"
)

// doctorReport 是 doctor 的 --json 输出（字段名属对外契约，见 docs/CLI.md）。
type doctorReport struct {
	OK      bool          `json:"ok"`
	Config  string        `json:"config"`
	Checks  []checkResult `json:"checks"`
	Summary summaryCounts `json:"summary"`
}

// newDoctorCommand 是「启动之前先问一遍」的离线自检。
func newDoctorCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "离线自检：配置、角色、目录、端口、外部命令、凭据、前端资源",
		Long: "在启动之前把「跑不起来」的原因问出来。\n\n" +
			"纯离线：不联网、不建目录、不落文件（端口探测会瞬时 bind 后立即释放）；\n" +
			"凭据只判有没有配，不验证有效性。\n" +
			"退出码 0 = 没有失败项，警告与跳过都不算失败。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := newPrinter(cmd, opts)
			result := runDoctor(opts.configPath)

			if p.json {
				if err := p.writeJSON(result); err != nil {
					return err
				}
			} else {
				p.printChecks(result.Checks)
				p.printSummary(result.Summary)
			}

			if !result.OK {
				// 结果已经输出过了，这里只需要一个非零退出码
				return errReported
			}

			return nil
		},
	}
}
