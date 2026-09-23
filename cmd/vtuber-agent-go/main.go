// vtuber-agent-go 是网关的唯一入口；子命令在 internal/cli 里组装。
//
// 裸跑只打帮助，启动服务用 `vtuber-agent-go run`（见 docs/CLI.md）。
package main

import (
	"os"

	"github.com/Agentropism/vtuber-agent-go/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
