package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// 版本信息由构建时的 -ldflags -X 注入，源码里的默认值让直接 go build 的产物也能正常输出。
// 注入方式见 docs/CLI.md；未注入的字段说「未知」，不造假值。
var (
	Version   = "dev"
	Commit    = ""
	BuildTime = ""
)

// versionInfo 是 version 的 --json 输出（字段名属对外契约，见 docs/CLI.md）。
type versionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

func newVersionCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "打印版本、commit、构建时间与 Go 版本",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := newPrinter(cmd, opts)
			info := versionInfo{
				Version:   Version,
				Commit:    Commit,
				BuildTime: BuildTime,
				GoVersion: runtime.Version(),
			}

			if p.json {
				return p.writeJSON(info)
			}

			fmt.Fprintf(p.out, "版本     %s\n", orUnknown(info.Version))
			fmt.Fprintf(p.out, "commit   %s\n", orUnknown(info.Commit))
			fmt.Fprintf(p.out, "构建时间 %s\n", orUnknown(info.BuildTime))
			fmt.Fprintf(p.out, "Go       %s\n", info.GoVersion)

			return nil
		},
	}
}

func orUnknown(value string) string {
	if value == "" {
		return "未知"
	}

	return value
}
