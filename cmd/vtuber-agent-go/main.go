package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Agentropism/vtuber-agent-go/internal/backend/app"
)

func main() {
	// Ctrl-C / SIGTERM 触发优雅退出：停 HTTP 服务，并收尾已建立的连接
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.Initialize()
	if err != nil {
		log.Fatal(err)
	}

	if err := application.Run(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("vtuber-agent-go 已退出")
}
