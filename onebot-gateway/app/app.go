package app

import (
	"fmt"
	"net/http"

	"onebot-gateway/agent/conversation"
	"onebot-gateway/agent/conversation/llm"
	"onebot-gateway/config"
	"onebot-gateway/gateway/distillery"
	"onebot-gateway/gateway/event"
	"onebot-gateway/gateway/server"
	"onebot-gateway/gateway/upload"
	"onebot-gateway/logger"

	"go.uber.org/zap"
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

	// 事件终端改为进程内 agent 会话：上传管线只负责去重、敏感词与背压，
	// 出口是本地方法调用，不再连远端 WebSocket。
	sessions, err := provideSessions(cfg, log)
	if err != nil {
		return nil, err
	}
	if sessions != nil {
		upload.SetHandler(sessions.Handle)
	}

	if err := upload.Init(upload.Options{
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

// provideSessions 装配本地 agent 会话层。
//
// 未配置 [llm] 的 base_url / model 时返回 nil：此时上传管线没有终端处理器，
// 事件会被丢弃并记录日志，而不是让网关启动失败。
func provideSessions(cfg *config.Config, log *zap.Logger) (*conversation.Sessions, error) {
	if cfg.LLM.BaseURL == "" || cfg.LLM.Model == "" {
		log.Sugar().Warn("未配置 [llm] 的 base_url / model，跳过 agent 会话初始化，事件不会被处理")
		return nil, nil
	}

	client, err := llm.New(llm.Config{
		BaseURL:     cfg.LLM.BaseURL,
		APIKey:      cfg.LLM.APIKey,
		Model:       cfg.LLM.Model,
		Temperature: cfg.LLM.Temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 LLM 客户端: %w", err)
	}

	conversation.SetLogger(log)

	sessions, err := conversation.NewSessions(conversation.SessionsConfig{
		LLM:           client,
		System:        cfg.Agent.SystemPrompt,
		InterruptMode: conversation.InterruptMode(cfg.Agent.InterruptMode),
		MaxToolRounds: cfg.Agent.MaxToolRounds,
		QueueSize:     cfg.Agent.QueueSize,
		TurnTimeout:   cfg.Agent.TurnTimeout,
		Reply:         server.SendAction,
	})
	if err != nil {
		return nil, fmt.Errorf("创建会话管理器: %w", err)
	}

	log.Sugar().Infof("agent 会话已启用: model=%s base_url=%s", cfg.LLM.Model, cfg.LLM.BaseURL)
	return sessions, nil
}
