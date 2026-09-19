package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/agent/conversation"
	"github.com/Agentropism/vtuber-agent-go/internal/agent/conversation/llm"
	"github.com/Agentropism/vtuber-agent-go/internal/agent/frontend"
	"github.com/Agentropism/vtuber-agent-go/internal/agent/memory"
	"github.com/Agentropism/vtuber-agent-go/internal/agent/tool"
	"github.com/Agentropism/vtuber-agent-go/internal/config"
	"github.com/Agentropism/vtuber-agent-go/internal/gateway/event"
	"github.com/Agentropism/vtuber-agent-go/internal/gateway/server"
	"github.com/Agentropism/vtuber-agent-go/internal/gateway/upload"
	"github.com/Agentropism/vtuber-agent-go/internal/logger"

	"go.uber.org/zap"
)

// App 是装配完成的进程：一个 HTTP 服务，外加随 ctx 存活的接入任务。
type App struct {
	Server *http.Server
	log    *zap.Logger
}

// Initialize 装配全部组件。
//
// ctx 决定后台任务的生命周期：B 站开放平台长连接随它启动、随它取消而收尾
// （取消时会调 end 接口结束互动会话）。
func Initialize(ctx context.Context) (*App, error) {
	started := time.Now()

	cfg, err := config.ProvideConfig()
	if err != nil {
		return nil, err
	}

	log, err := logger.ProvideLogger(cfg)
	if err != nil {
		return nil, err
	}

	// 角色资产：读不到就在启动阶段失败，不拖到第一次对话
	character, err := config.LoadCharacter(cfg.Agent.CharacterFile)
	if err != nil {
		return nil, err
	}
	if character.Name != "" {
		log.Sugar().Infof("已加载角色: %s（Live2D 模型 %s）", character.Name, character.Live2DModel)
	}

	upload.SetLogger(log)

	// 前端接入：浏览器页面 + /client-ws + 模型静态资源；未配置时为 nil
	front, err := provideFrontend(cfg, character, log)
	if err != nil {
		return nil, err
	}

	// 语音播报：TTS 引擎链 + 统一播报队列；未配置 [tts].engines 时为 nil
	queue, err := provideBroadcast(cfg, log, front)
	if err != nil {
		return nil, err
	}
	// /inject 与事件回复共用这一条队列；未装配播报时保持未注入，接口返回 503
	if queue != nil {
		server.SetInjectHandler(provideInject(queue))
	}

	// 长期记忆：JSON Lines 存储 + 关键词召回；未配置路径时为 nil
	store, err := provideMemory(cfg, log)
	if err != nil {
		return nil, err
	}

	// 工具注册层：记忆检索与状态查询，交给会话层的工具调用循环驱动
	registry := provideTools(cfg, log, store, front, queue, started)

	// 事件终端改为进程内 agent 会话：上传管线只负责去重、敏感词与背压，
	// 出口是本地方法调用，不再连远端 WebSocket。
	sessions, err := provideSessions(cfg, log, character, queue, front, store, registry)
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

	Register()
	event.SetLogger(log)

	// B 站开放平台长连接：事件在进程内直接进分发链，等价于一个内置的接入客户端
	biliClient, err := provideBilibili(cfg, log)
	if err != nil {
		return nil, err
	}
	if biliClient != nil {
		go func() {
			if err := biliClient.Run(ctx); err != nil && ctx.Err() == nil {
				log.Sugar().Errorf("B 站接入已停止: %v", err)
			}
		}()
	}

	return &App{Server: server.ProvideServer(cfg, log, frontendRoutes(front)...), log: log}, nil
}

// frontendRoutes 把前端接入挂到网关 mux 上。
//
// 页面挂在 /web/ 而不是 /：接入客户端可以占用根路径（[[clients]].path），
// 两者的路由不该互相打架。
func frontendRoutes(front *frontend.Frontend) []server.Route {
	if front == nil {
		return nil
	}

	return []server.Route{
		{Pattern: "/favicon.ico", Handler: front.FaviconHandler()},
		{Pattern: "/client-ws", Handler: front.ClientWSHandler()},
		{Pattern: "/live2d-models/", Handler: front.ModelsHandler()},
		{Pattern: "/web/", Handler: front.WebHandler()},
	}
}

// shutdownTimeout 是优雅退出的等待上限。
const shutdownTimeout = 10 * time.Second

// Run 启动 HTTP 服务并阻塞，直到 ctx 取消或服务出错。
//
// ctx 取消时先停 HTTP 服务，再等后台任务收尾（B 站接入会在这里调 end 接口）。
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() { errCh <- a.Server.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		a.log.Sugar().Info("收到退出信号，正在停止服务")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := a.Server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("停止 HTTP 服务: %w", err)
	}

	return nil
}

// provideSessions 装配本地 agent 会话层。
//
// 未配置 [llm] 的 base_url / model 时返回 nil：此时上传管线没有终端处理器，
// 事件会被丢弃并记录日志，而不是让网关启动失败。
//
// 系统提示词的优先级：角色文件的 system_prompt > [agent].system_prompt > 内置兜底值。
func provideSessions(
	cfg *config.Config,
	log *zap.Logger,
	character config.Character,
	queue *broadcast.Queue,
	front *frontend.Frontend,
	store *memory.Store,
	registry *tool.Registry,
) (*conversation.Sessions, error) {
	if cfg.LLM.BaseURL == "" || cfg.LLM.Model == "" {
		log.Sugar().Warn("未配置 [llm] 的 base_url / model，跳过 agent 会话初始化，事件不会被处理")
		return nil, nil
	}

	system := cfg.Agent.SystemPrompt
	if strings.TrimSpace(character.SystemPrompt) != "" {
		system = character.SystemPrompt
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

	sessionsCfg := conversation.SessionsConfig{
		LLM:              client,
		System:           system,
		InterruptMode:    conversation.InterruptMode(cfg.Agent.InterruptMode),
		MaxToolRounds:    cfg.Agent.MaxToolRounds,
		MaxHistoryTurns:  cfg.Agent.HistoryMaxTurns,
		MaxHistoryTokens: cfg.Agent.HistoryMaxTokens,
		QueueSize:        cfg.Agent.QueueSize,
		TurnTimeout:      cfg.Agent.TurnTimeout,
		Reply:            server.SendAction,
	}
	// 注意：typed nil 装进接口后不等于 nil，必须先判空
	if queue != nil {
		sessionsCfg.Broadcast = queue
	}
	// 表情词表由前端（模型清单）提供；没有前端就没有表情，标签也不做特殊处理
	if front != nil {
		sessionsCfg.Emotions = front.Emotions()
	}
	if store != nil {
		sessionsCfg.Memory = memoryAdapter{store: store, log: log}
		sessionsCfg.RecallLimit = cfg.Agent.RecallLimit
	}
	if registry != nil {
		sessionsCfg.Tools = registry.Tools()
		sessionsCfg.ToolExecutor = registry
	}
	sessionsCfg.IdleSpeakInterval = cfg.Agent.IdleSpeakInterval
	sessionsCfg.IdleSpeakPrompt = cfg.Agent.IdleSpeakPrompt

	sessions, err := conversation.NewSessions(sessionsCfg)
	if err != nil {
		return nil, fmt.Errorf("创建会话管理器: %w", err)
	}

	log.Sugar().Infof("agent 会话已启用: model=%s base_url=%s", cfg.LLM.Model, cfg.LLM.BaseURL)
	return sessions, nil
}
