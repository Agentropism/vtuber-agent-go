package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation/llm"
)

func echoTool() llm.Tool {
	return llm.Tool{
		Name:        "echo",
		Description: "原样返回输入",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
	}
}

func TestRegisterAndTools(t *testing.T) {
	registry := New()
	if err := registry.Register(echoTool(), func(_ context.Context, args json.RawMessage) (string, error) {
		return string(args), nil
	}); err != nil {
		t.Fatalf("注册: %v", err)
	}

	tools := registry.Tools()
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("工具清单 = %#v", tools)
	}
	if names := registry.Names(); len(names) != 1 || names[0] != "echo" {
		t.Fatalf("工具名 = %v", names)
	}
}

func TestRegisterRejectsBadInput(t *testing.T) {
	registry := New()

	if err := registry.Register(llm.Tool{}, func(context.Context, json.RawMessage) (string, error) { return "", nil }); err == nil {
		t.Fatal("空工具名应报错")
	}
	if err := registry.Register(echoTool(), nil); err == nil {
		t.Fatal("没有处理器应报错")
	}
}

func TestExecuteReturnsToolMessages(t *testing.T) {
	registry := New()
	_ = registry.Register(echoTool(), func(_ context.Context, args json.RawMessage) (string, error) {
		return "结果:" + string(args), nil
	})

	messages, err := registry.Execute(context.Background(), []llm.ToolCall{
		{ID: "call-1", Name: "echo", Arguments: `{"text":"你好"}`},
	})
	if err != nil {
		t.Fatalf("执行: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("结果消息数 = %d, want 1", len(messages))
	}
	if messages[0].Role != llm.RoleTool || messages[0].ToolCallID != "call-1" {
		t.Fatalf("结果消息字段错误: %#v", messages[0])
	}
	if messages[0].Content != `结果:{"text":"你好"}` {
		t.Fatalf("结果内容 = %q", messages[0].Content)
	}
}

// 工具报错不中断整轮：错误折成文本回灌，模型自己决定怎么圆。
func TestExecuteFoldsErrorsIntoResult(t *testing.T) {
	registry := New()
	_ = registry.Register(echoTool(), func(context.Context, json.RawMessage) (string, error) {
		return "", fmt.Errorf("下游超时")
	})

	messages, err := registry.Execute(context.Background(), []llm.ToolCall{{ID: "c1", Name: "echo"}})
	if err != nil {
		t.Fatalf("执行不应返回错误: %v", err)
	}
	if len(messages) != 1 || messages[0].Content == "" {
		t.Fatalf("应把错误折成结果: %#v", messages)
	}
}

func TestExecuteUnknownTool(t *testing.T) {
	registry := New()

	messages, err := registry.Execute(context.Background(), []llm.ToolCall{{ID: "c1", Name: "nope"}})
	if err != nil {
		t.Fatalf("执行不应返回错误: %v", err)
	}
	if len(messages) != 1 || messages[0].Content == "" {
		t.Fatalf("未知工具也要给出可回灌的结果: %#v", messages)
	}
}

// 参数是坏 JSON 时按空对象处理，不要把它丢给下游。
func TestExecuteRepairsBadArguments(t *testing.T) {
	registry := New()
	var received string
	_ = registry.Register(echoTool(), func(_ context.Context, args json.RawMessage) (string, error) {
		received = string(args)
		return "ok", nil
	})

	if _, err := registry.Execute(context.Background(), []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: "{坏 JSON"}}); err != nil {
		t.Fatalf("执行: %v", err)
	}
	if received != "{}" {
		t.Fatalf("坏参数应被替换成空对象，实际 %q", received)
	}
}

func TestExecuteEmptyCalls(t *testing.T) {
	registry := New()

	messages, err := registry.Execute(context.Background(), nil)
	if err != nil || messages != nil {
		t.Fatalf("空调用应返回空结果: %#v, %v", messages, err)
	}
}
