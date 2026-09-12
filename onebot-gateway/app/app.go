package app

import (
	"net/http"

	"onebot-gateway/config"
	"onebot-gateway/gateway/distillery"
	"onebot-gateway/gateway/event"
	"onebot-gateway/gateway/server"
	"onebot-gateway/gateway/upload"
	"onebot-gateway/logger"
)

func Initialize() (*http.Server, error) {
	cfg, err := config.ProvideConfig()
	if err != nil {
		return nil, err
	}

	log, err := logger.ProvideLogger(cfg)
	if err != nil {
		return nil, err
	}

	upload.SetLogger(log)
	// 注入远程 Action 转发回调（server.SendAction），解除 server↔upload 循环依赖
	upload.SetActionForwarder(server.SendAction)
	if err := upload.Init(cfg.Memory.Target, cfg.Memory.CallbackPlatform, upload.Options{
		QueueSize:          cfg.Memory.QueueSize,
		QueueWaitTimeout:   cfg.Memory.QueueWaitTimeout,
		SensitiveWordsFile: cfg.Memory.SensitiveWordsFile,
		DedupTTL:           cfg.Memory.DedupTTL,
	}); err != nil {
		return nil, err
	}
	distillery.SetLogger(log)
	distillery.Init(cfg.Bilibili.DistilleryTarget)

	Register()
	event.SetLogger(log)
	httpServer := server.ProvideServer(cfg, log)
	return httpServer, nil
}
