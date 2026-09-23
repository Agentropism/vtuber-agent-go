package cli

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Agentropism/vtuber-agent-go/internal/backend/app"
)

// newRunCommand 是原来的裸跑行为：装配并启动服务，直到收到 SIGINT / SIGTERM。
func newRunCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "启动服务（HTTP 接入、会话与 LLM、播报队列、内置前端）",
		Long: "装配并启动服务，直到收到 SIGINT / SIGTERM 后优雅退出。\n" +
			"配置里的相对路径（characters/、data/）以当前工作目录为基准。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			application, err := app.Initialize(opts.configPath)
			if err != nil {
				return err
			}

			if err := application.Run(ctx); err != nil {
				return err
			}

			// 这行日志是「走完了优雅退出分支」的标志（人工排查与脚本都可以据此判断）。
			log.Println("vtuber-agent-go 已退出")

			return nil
		},
	}
}
