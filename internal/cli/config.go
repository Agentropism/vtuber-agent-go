package cli

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
)

// templateFileName 是 config init 的模板来源，相对当前工作目录读取。
//
// 不做内嵌副本：go:embed 不能引用包外路径，而把模板写成 Go 常量就等于多一份
// 「配置长什么样」的事实源，迟早与 config.toml.example 对不上。
const templateFileName = "config.toml.example"

// newConfigCommand 只管生成与查看；配置合法性校验统一归 doctor，避免两边结论不一致。
func newConfigCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "生成与查看配置",
		Long:  "配置的生成（init）与查看（show）。配置合法性校验归 doctor，这里不重复做。",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cmd.Help()

			return errReported
		},
	}

	cmd.AddCommand(newConfigInitCommand(opts), newConfigShowCommand(opts))

	return cmd
}

func newConfigInitCommand(opts *globalOptions) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "从当前目录的 config.toml.example 生成配置文件",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := newPrinter(cmd, opts)
			target := opts.configPath

			// 默认拒绝覆盖：目标里可能已经填了凭据，手滑重跑一次就能把它洗掉。
			if _, err := os.Stat(target); err == nil && !force {
				return fmt.Errorf("%s 已存在；要覆盖请加 --force", target)
			}

			data, err := os.ReadFile(templateFileName)
			if err != nil {
				return fmt.Errorf("读取模板 %s 失败（模板取自当前工作目录）: %w", templateFileName, err)
			}

			// 0600：生成的配置里会填 API key 与登录态。
			if err := os.WriteFile(target, data, 0o600); err != nil {
				return fmt.Errorf("写入 %s 失败: %w", target, err)
			}

			if p.json {
				return p.writeJSON(map[string]any{
					"path":    target,
					"source":  templateFileName,
					"created": true,
				})
			}

			fmt.Fprintf(p.out, "已生成 %s（来自 %s，权限 0600）。\n", target, templateFileName)
			fmt.Fprintf(p.out, "下一步：填好 [llm] 等必填项，然后跑 doctor 自检。\n")

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "目标已存在时覆盖")

	return cmd
}

func newConfigShowCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "打印当前生效的配置（密钥只回是否已配置）",
		Long: "打印当前生效的配置快照，含环境变量补齐后的凭据状态。\n" +
			"密钥类字段（api_key、cookie）只回 {\"configured\": bool}，永不回明文；\n" +
			"--json 与 GET /api/config 同构。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := newPrinter(cmd, opts)

			cfg, err := config.ProvideConfigFrom(opts.configPath)
			if err != nil {
				return err
			}

			snapshot := config.RedactedMap(cfg)
			if p.json {
				return p.writeJSON(snapshot)
			}

			// 人读用 TOML：键名与 config.toml 同构，方便对着配置文件看。
			return toml.NewEncoder(p.out).Encode(snapshot)
		},
	}
}
