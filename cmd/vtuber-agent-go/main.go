package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Agentropism/vtuber-agent-go/internal/app"
)

func main() {
	// Ctrl-C / SIGTERM 触发优雅退出：停 HTTP 服务，并结束 B 站互动会话
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.Initialize(ctx)
	if err != nil {
		log.Fatal(err)
	}

	if err := application.Run(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("vtuber-agent-go 已退出")
}
