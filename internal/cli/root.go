package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// errReported 表示结果已经打印过了，只需要一个非零退出码（不再重复报错）。
var errReported = errors.New("结果已输出")

// exitFailure 是失败时的退出码。契约只有 0 与 1 两档（见 docs/CLI.md）。
const exitFailure = 1

// globalOptions 是所有子命令都认的参数。
type globalOptions struct {
	configPath string
	json       bool
}

// NewRootCommand 组装整个命令行。
func NewRootCommand() *cobra.Command {
	opts := &globalOptions{}

	root := &cobra.Command{
		Use:   "vtuber-agent-go",
		Short: "实时语音交互 VTuber 网关",
		Long: "单二进制网关：接入 QQ / B站 事件，跑会话与 LLM，统一播报队列，云 TTS 出声，" +
			"并把 Live2D 页面托管在本地。",
		Args: cobra.NoArgs,
		// 裸跑不启动服务：只打帮助并以 1 退出，启动服务要显式写 run。
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cmd.Help()

			return errReported
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVarP(&opts.configPath, "config", "c", "config.toml", "配置文件路径")
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "输出 JSON（字段名见 docs/CLI.md）")
	root.PersistentFlags().BoolP("help", "h", false, "显示帮助")

	// cobra 默认会挂一个 completion 子命令：本轮命令少，补全价值有限，隐藏它保持命令表干净。
	root.CompletionOptions.DisableDefaultCmd = true
	// 帮助模板改中文，与「注释与日志一律中文」的约定一致。
	root.SetUsageTemplate(usageTemplate)
	root.SetHelpTemplate(helpTemplate)

	root.AddCommand(
		newRunCommand(opts),
		newConfigCommand(opts),
		newDoctorCommand(opts),
		newVersionCommand(opts),
	)

	// 内置的 help 子命令是英文文案，这里补中文说明。
	// 必须在 AddCommand 之后调用：InitDefaultHelpCmd 在没有子命令时直接返回。
	root.InitDefaultHelpCmd()
	for _, sub := range root.Commands() {
		if sub.Name() == "help" {
			sub.Short = "查看子命令的帮助"
		}
	}

	return root
}

// Execute 运行命令行并返回进程退出码。
func Execute() int {
	return execute(NewRootCommand(), os.Stderr)
}

// execute 是 Execute 的可测内核：错误信息写到 errOut，返回值就是进程退出码。
func execute(cmd *cobra.Command, errOut io.Writer) int {
	if err := cmd.Execute(); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintf(errOut, "错误: %v\n", err)
		}

		return exitFailure
	}

	return 0
}

// usageTemplate / helpTemplate 是 cobra 默认模板的中文化版本：标签与提示语都换成中文。
const usageTemplate = `用法:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [子命令]{{end}}{{if gt (len .Aliases) 0}}

别名:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

可用子命令:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

子命令参数:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局参数:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

补充帮助:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

用 "{{.CommandPath}} [子命令] --help" 查看某个子命令的详细用法。{{end}}
`

const helpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`
