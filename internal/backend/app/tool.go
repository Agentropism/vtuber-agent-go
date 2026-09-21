package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/backend/web"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation/llm"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/memory"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/tool"
	"github.com/Agentropism/vtuber-agent-go/internal/core/config"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// provideTools 装配工具注册层。
//
// 只注册当前真的可用的工具：没配记忆就不给记忆检索工具——给模型一个必然失败的
// 工具，只会浪费一轮调用。
func provideTools(
	cfg *config.Config,
	store *memory.Store,
	front *web.Frontend,
	queue *broadcast.Queue,
	started time.Time,
) *tool.Registry {
	if !cfg.Agent.EnableTools {
		logger.Info("未开启 [agent].enable_tools，跳过工具注册")
		return nil
	}

	registry := tool.New()

	if store != nil {
		if err := registry.Register(memorySearchTool(), memorySearchHandler(store)); err != nil {
			logger.Warnf("注册记忆检索工具失败: %v", err)
		}
	}
	if err := registry.Register(statusTool(), statusHandler(front, queue, started)); err != nil {
		logger.Warnf("注册状态查询工具失败: %v", err)
	}

	logger.Infof("工具已注册: %v", registry.Names())
	return registry
}

// memorySearchTool 是记忆检索工具的定义。
func memorySearchTool() llm.Tool {
	return llm.Tool{
		Name:        "memory_search",
		Description: "检索以前的对话记录。当观众提到过去说过的事、或你需要回忆某人说过什么时使用。",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "检索关键词"},
				"limit": {"type": "integer", "description": "返回条数，默认 3"}
			},
			"required": ["query"]
		}`),
	}
}

// memorySearchHandler 执行记忆检索，把命中记录拼成一段文本。
func memorySearchHandler(store *memory.Store) tool.Handler {
	return func(_ context.Context, args json.RawMessage) (string, error) {
		var params struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return "", fmt.Errorf("参数解析失败: %w", err)
		}
		if params.Query == "" {
			return "没有给出检索关键词", nil
		}
		if params.Limit <= 0 {
			params.Limit = 3
		}

		hits := store.Recall(params.Query, params.Limit)
		if len(hits) == 0 {
			return "没有找到相关记录", nil
		}

		out := ""
		for _, hit := range hits {
			out += fmt.Sprintf("- %s 说：%s（你回复：%s）\n", hit.User, hit.Text, hit.Reply)
		}

		return out, nil
	}
}

// statusTool 是状态查询工具的定义。
func statusTool() llm.Tool {
	return llm.Tool{
		Name:        "bot_status",
		Description: "查询自己当前的运行状态：已运行时长、前端连接数、播报队列计数。",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

// statusHandler 汇总当前运行状态。
func statusHandler(front *web.Frontend, queue *broadcast.Queue, started time.Time) tool.Handler {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		clients := 0
		if front != nil {
			clients = front.ClientCount()
		}

		status := fmt.Sprintf("已运行 %s；前端连接数 %d。",
			time.Since(started).Round(time.Second), clients)
		if queue != nil {
			stats := queue.Stats()
			status += fmt.Sprintf(" 播报队列：入队 %d、已播 %d、被抢占 %d、丢弃 %d、失败 %d。",
				stats.Enqueued, stats.Played, stats.Preempted, stats.Dropped, stats.Failed)
		}

		return status, nil
	}
}
