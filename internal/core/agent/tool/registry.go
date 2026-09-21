// Package tool 提供工具注册层（执行计划 E7）。
//
// 原 Python 侧靠 MCP 提示词模式实现工具调用，那需要第二个循环与一套提示词协议；
// 这里按计划 Q5 重建为纯注册层：注册「模型能看懂的工具定义」与「本地怎么执行」，
// 由会话层已有的工具调用循环驱动（Agent 已经会「执行工具 → 回灌结果 → 继续生成」）。
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation/llm"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// Handler 执行一次工具调用，返回要回灌给模型的文本。
type Handler func(ctx context.Context, args json.RawMessage) (string, error)

// Registry 是工具注册表；它实现了会话层要的 ToolExecutor。
type Registry struct {
	mu       sync.RWMutex
	order    []string
	defs     map[string]llm.Tool
	handlers map[string]Handler
}

// New 构造空的注册表。
func New() *Registry {
	return &Registry{
		defs:     make(map[string]llm.Tool),
		handlers: make(map[string]Handler),
	}
}

// Register 注册一个工具。同名重复注册会覆盖，并保持原有顺序。
func (r *Registry) Register(def llm.Tool, handler Handler) error {
	if def.Name == "" {
		return errors.New("tool: 工具名不能为空")
	}
	if handler == nil {
		return fmt.Errorf("tool: 工具 %s 没有处理器", def.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.defs[def.Name]; !exists {
		r.order = append(r.order, def.Name)
	}
	r.defs[def.Name] = def
	r.handlers[def.Name] = handler

	return nil
}

// Tools 返回按注册顺序排列的工具定义，交给模型声明。
func (r *Registry) Tools() []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		tools = append(tools, r.defs[name])
	}

	return tools
}

// Names 返回已注册的工具名（已排序，便于日志与测试）。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := append([]string(nil), r.order...)
	sort.Strings(names)

	return names
}

// Execute 依次执行模型要求的工具调用，返回要回灌的结果消息。
//
// 单个工具失败不中断整轮：把错误文本当作工具结果回灌，让模型自己决定怎么圆场
// （这是工具调用的常规做法，比直接让整轮对话失败更稳）。
func (r *Registry) Execute(ctx context.Context, calls []llm.ToolCall) ([]llm.Message, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	r.mu.RLock()
	handlers := make(map[string]Handler, len(calls))
	for _, call := range calls {
		handlers[call.ID] = r.handlers[call.Name]
	}
	r.mu.RUnlock()

	results := make([]llm.Message, 0, len(calls))
	for _, call := range calls {
		content := r.runOne(ctx, call, handlers[call.ID])
		results = append(results, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: call.ID,
			Content:    content,
		})
	}

	return results, nil
}

// runOne 执行一次调用，把结果或错误都折成文本。
func (r *Registry) runOne(ctx context.Context, call llm.ToolCall, handler Handler) string {
	if handler == nil {
		logger.Warnf("模型要求调用未注册的工具: %s", call.Name)
		return fmt.Sprintf("没有名为 %s 的工具", call.Name)
	}

	args := json.RawMessage(call.Arguments)
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if !json.Valid(args) {
		args = json.RawMessage("{}")
	}

	result, err := handler(ctx, args)
	if err != nil {
		logger.Warnf("工具 %s 执行失败: %v", call.Name, err)
		return fmt.Sprintf("工具 %s 执行失败: %v", call.Name, err)
	}

	logger.Debugf("工具 %s 执行完成，返回 %d 字节", call.Name, len(result))
	return result
}
