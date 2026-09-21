package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/backend/api"
	"github.com/Agentropism/vtuber-agent-go/internal/backend/server"
	"github.com/Agentropism/vtuber-agent-go/internal/backend/web"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation/llm"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/memory"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/tool"
	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/upload"
	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// App 是装配完成的进程：一个 HTTP 服务。
type App struct {
	Server *http.Server
	stream *streamRuntime
}

// Initialize 装配全部组件。
//
// 接入端（B 站上报端、QQ 适配端）都是外部进程，经 WebSocket 接入；这里不启动后台任务。
func Initialize() (*App, error) {
	started := time.Now()

	cfg, err := config.ProvideConfig()
	if err != nil {
		return nil, err
	}

	if _, err := logger.ProvideLogger(cfg); err != nil {
		return nil, err
	}

	// 角色资产：读不到就在启动阶段失败，不拖到第一次对话
	character, err := config.LoadCharacter(cfg.Agent.CharacterFile)
	if err != nil {
		return nil, err
	}
	if character.Name != "" {
		logger.Infof("已加载角色: %s（Live2D 模型 %s）", character.Name, character.Live2DModel)
	}

	// 前端接入：浏览器页面 + /client-ws + 模型静态资源；未配置时为 nil
	front, err := provideFrontend(cfg, character)
	if err != nil {
		return nil, err
	}

	// 推流：开播取地址 + 虚拟屏渲染 + ffmpeg 推 RTMP；未启用 [stream] 时为 nil
	streaming, err := provideStream(cfg)
	if err != nil {
		return nil, err
	}

	// 扫码登录：只为推流取开播凭据，未启用 [stream] 时为 nil
	login := provideLogin(cfg, streaming)
	if login != nil {
		logger.Infof("扫码登录页（仅本机可访问）: %s", loginURL(cfg.Server.Addr))
	}
	// 语音播报：TTS 引擎链 + 统一播报队列；未配置 [tts].engines 时为 nil
	queue, err := provideBroadcast(cfg, front, streaming)
	if err != nil {
		return nil, err
	}
	// 播报注入（/api/speak，过渡期还含旧路径 /inject）与事件回复共用这一条队列；
	// 未装配播报时 speak 为 nil，接口返回 503。装配成路由见下方的 apiHandler。
	var speak func(text, emotion string) error
	if queue != nil {
		speak = provideSpeak(queue)
	}

	// 长期记忆：JSON Lines 存储 + 关键词召回；未配置路径时为 nil
	store, err := provideMemory(cfg)
	if err != nil {
		return nil, err
	}

	// 工具注册层：记忆检索与状态查询，交给会话层的工具调用循环驱动
	registry := provideTools(cfg, store, front, queue, started)

	// 事件终端改为进程内 agent 会话：上传管线只负责去重、敏感词与背压，
	// 出口是本地方法调用，不再连远端 WebSocket。
	sessions, err := provideSessions(cfg, character, queue, front, store, registry)
	if err != nil {
		return nil, err
	}
	if sessions != nil {
		upload.SetHandler(sessions.Handle)
	}

	// 前端接口层：配置只读、播报注入、会话读写、运行状态。
	// 能力用 nil 表示未装配（没配 TTS 就没有播报、没配 LLM 就没有会话），接口据此返回 503。
	apiHandler := &api.Handler{
		Config:   config.ProvideConfig,
		Speak:    speak,
		Sessions: sessions,
		Queue:    queue,
		Clients:  server.ConnectedPlatforms,
		Upload:   upload.Stats,
		Started:  started,
	}

	if err := upload.Init(upload.Options{
		QueueSize:          cfg.Memory.QueueSize,
		QueueWaitTimeout:   cfg.Memory.QueueWaitTimeout.Std(),
		SensitiveWordsFile: cfg.Memory.SensitiveWordsFile,
		DedupTTL:           cfg.Memory.DedupTTL.Std(),
	}); err != nil {
		return nil, err
	}

	Register()

	routes := frontendRoutes(front)
	routes = append(routes, apiHandler.Routes()...)
	if login != nil {
		routes = append(routes, login.routes()...)
	}

	return &App{Server: server.ProvideServer(cfg, routes...), stream: streaming}, nil
}

// frontendRoutes 把前端接入挂到网关 mux 上。
//
// 页面挂在 /web/ 而不是 /：接入客户端可以占用根路径（[[clients]].path），
// 两者的路由不该互相打架。
func frontendRoutes(front *web.Frontend) []server.Route {
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
// ctx 取消时先停 HTTP 服务，再等已建立的连接收尾。
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() { errCh <- a.Server.ListenAndServe() }()

	// 推流随进程存活：地址解析、虚拟屏与 ffmpeg 都在这里起，退出时一并收掉
	if a.stream != nil {
		defer a.stream.Close()

		var streaming sync.WaitGroup
		streaming.Add(1)
		go func() {
			defer streaming.Done()
			if err := a.stream.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Errorf("推流已停止: %v", err)
			}
		}()

		// 退出前必须等推流协程收尾：它的 defer 里要调关播接口。进程先走就等于把
		// 直播间挂在「直播中」（实测踩到：进程已退出，B 站侧 live_status 仍是 1、还挂着观众）。
		defer func() {
			done := make(chan struct{})
			go func() { streaming.Wait(); close(done) }()

			select {
			case <-done:
			case <-time.After(shutdownTimeout):
				logger.Warn("等推流收尾超时，关播可能未完成")
			}
		}()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("收到退出信号，正在停止服务")
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
	character config.Character,
	queue *broadcast.Queue,
	front *web.Frontend,
	store *memory.Store,
	registry *tool.Registry,
) (*conversation.Sessions, error) {
	if cfg.LLM.BaseURL == "" || cfg.LLM.Model == "" {
		logger.Warn("未配置 [llm] 的 base_url / model，跳过 agent 会话初始化，事件不会被处理")
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

	sessionsCfg := conversation.SessionsConfig{
		LLM:              client,
		System:           system,
		InterruptMode:    conversation.InterruptMode(cfg.Agent.InterruptMode),
		MaxToolRounds:    cfg.Agent.MaxToolRounds,
		MaxHistoryTurns:  cfg.Agent.HistoryMaxTurns,
		MaxHistoryTokens: cfg.Agent.HistoryMaxTokens,
		QueueSize:        cfg.Agent.QueueSize,
		TurnTimeout:      cfg.Agent.TurnTimeout.Std(),
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
		sessionsCfg.Memory = memoryAdapter{store: store}
		sessionsCfg.RecallLimit = cfg.Agent.RecallLimit
	}
	if registry != nil {
		sessionsCfg.Tools = registry.Tools()
		sessionsCfg.ToolExecutor = registry
	}
	sessionsCfg.IdleSpeakInterval = cfg.Agent.IdleSpeakInterval.Std()
	sessionsCfg.IdleSpeakPrompt = cfg.Agent.IdleSpeakPrompt

	sessions, err := conversation.NewSessions(sessionsCfg)
	if err != nil {
		return nil, fmt.Errorf("创建会话管理器: %w", err)
	}

	logger.Infof("agent 会话已启用: model=%s base_url=%s", cfg.LLM.Model, cfg.LLM.BaseURL)
	return sessions, nil
}
