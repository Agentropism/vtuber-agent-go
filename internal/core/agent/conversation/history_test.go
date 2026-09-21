package conversation

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation/llm"
)

func historyUser(text string) llm.Message { return llm.Message{Role: llm.RoleUser, Content: text} }
func historyAssistant(text string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: text}
}

// turn 造一轮「用户 + 助手」，各 100 个汉字（≈100 token）。
func turn(userText, assistantText string) []llm.Message {
	return []llm.Message{
		historyUser(userText),
		historyAssistant(assistantText),
	}
}

func longText(prefix string) string {
	return prefix + strings.Repeat("字", 97)
}

func TestTrimHistoryByTurns(t *testing.T) {
	history := []llm.Message{
		historyUser("一"), historyAssistant("1"),
		historyUser("二"), historyAssistant("2"),
		historyUser("三"), historyAssistant("3"),
	}

	got := TrimHistory(history, HistoryLimits{MaxTurns: 2})

	want := []string{"二", "2", "三", "3"}
	if len(got) != len(want) {
		t.Fatalf("裁剪后长度 = %d, want %d（%#v）", len(got), len(want), got)
	}
	if got[0].Role != llm.RoleUser {
		t.Fatalf("裁剪后首条角色 = %s, want user", got[0].Role)
	}
	for i, text := range want {
		if got[i].Content != text {
			t.Fatalf("第 %d 条 = %q, want %q", i, got[i].Content, text)
		}
	}
}

// 轮数没超时不裁剪，且返回原切片（不做无谓拷贝）。
func TestTrimHistoryReturnsSameSliceWhenUnderLimits(t *testing.T) {
	history := turn("你好", "在的")

	got := TrimHistory(history, HistoryLimits{MaxTurns: 12, MaxTokens: 4000})
	if len(got) != len(history) {
		t.Fatalf("长度 = %d, want %d", len(got), len(history))
	}
	if &got[0] != &history[0] {
		t.Fatalf("未发生裁剪时应返回原切片")
	}
}

// 不限轮数时只按 token 预算裁。
func TestTrimHistoryByTokensDropsWholeTurns(t *testing.T) {
	history := []llm.Message{}
	history = append(history, turn(longText("一"), longText("1"))...)
	history = append(history, turn(longText("二"), longText("2"))...)
	history = append(history, turn(longText("三"), longText("3"))...)

	// 每轮约 200 token，预算 250 只装得下最后一轮。
	got := TrimHistory(history, HistoryLimits{MaxTokens: 250})

	if len(got) != 2 {
		t.Fatalf("裁剪后长度 = %d, want 2（%#v）", len(got), got)
	}
	if got[0].Role != llm.RoleUser || !strings.HasPrefix(got[0].Content, "三") {
		t.Fatalf("应保留最后一整轮，实际首条 = %#v", got[0])
	}
}

// 只剩一轮仍超预算时保留它：总得让模型看见用户刚说的话。
func TestTrimHistoryKeepsLastTurnEvenIfOverBudget(t *testing.T) {
	history := turn(longText("一"), longText("1"))

	got := TrimHistory(history, HistoryLimits{MaxTokens: 10})
	if len(got) != 2 {
		t.Fatalf("长度 = %d, want 2（最后一轮必须保留）", len(got))
	}
}

// 两个上限同时生效时按更严的那个裁。
func TestTrimHistoryAppliesBothLimits(t *testing.T) {
	history := []llm.Message{}
	history = append(history, turn(longText("一"), longText("1"))...)
	history = append(history, turn(longText("二"), longText("2"))...)
	history = append(history, turn(longText("三"), longText("3"))...)

	got := TrimHistory(history, HistoryLimits{MaxTurns: 2, MaxTokens: 250})

	if len(got) != 2 {
		t.Fatalf("长度 = %d, want 2", len(got))
	}
	if !strings.HasPrefix(got[0].Content, "三") {
		t.Fatalf("token 预算应压过轮数上限，实际首条 = %q", got[0].Content)
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"空串", "", 0},
		{"四个汉字", "你好世界", 4},
		{"英文按四字符一 token", "abcdefgh", 2},
		{"中英混排", "你好abcd", 3},
		{"全角标点算汉字", "你好，世界", 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := estimateTokens(tc.text); got != tc.want {
				t.Fatalf("estimateTokens(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

// 长对话跑很多轮后历史长度必须稳定在上限内——这正是裁剪要解决的问题。
func TestAgentHistoryStaysBounded(t *testing.T) {
	turns := make([][]llm.Event, 50)
	for i := range turns {
		turns[i] = []llm.Event{{Text: "好"}}
	}

	agent, err := NewAgent(AgentConfig{LLM: &scriptedClient{turns: turns}, MaxHistoryTurns: 3})
	if err != nil {
		t.Fatalf("构造 Agent: %v", err)
	}

	for i := 0; i < 50; i++ {
		text := "第" + strconv.Itoa(i) + "轮"
		if err := agent.Chat(context.Background(), text, func(string) error { return nil }); err != nil {
			t.Fatalf("第 %d 轮: %v", i, err)
		}
	}

	history := agent.History()
	if len(history) > 6 {
		t.Fatalf("50 轮之后历史长度 = %d, want <= 6（3 轮）", len(history))
	}
	if history[0].Role != llm.RoleUser {
		t.Fatalf("历史首条角色 = %s, want user", history[0].Role)
	}
}
