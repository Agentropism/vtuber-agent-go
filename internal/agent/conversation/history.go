package conversation

import (
	"github.com/Agentropism/vtuber-agent-go/internal/agent/conversation/llm"
)

// 历史裁剪的默认上限，配置未指定时生效。
const (
	// defaultMaxHistoryTurns 是历史保留的最近对话轮数。
	defaultMaxHistoryTurns = 12
	// defaultMaxHistoryTokens 是历史估算 token 上限。
	defaultMaxHistoryTokens = 4000
)

// HistoryLimits 是历史裁剪的两个上限，两者同时生效，谁先触发按谁裁。
type HistoryLimits struct {
	// MaxTurns 是保留的最近对话轮数（一条 user 消息起算一轮），<=0 表示不限。
	MaxTurns int
	// MaxTokens 是历史的估算 token 上限，<=0 表示不限。
	// 只统计历史本身，不含系统提示词。
	MaxTokens int
}

// 历史裁剪的默认上限：12 轮、4000 token。
func defaultHistoryLimits() HistoryLimits {
	return HistoryLimits{MaxTurns: defaultMaxHistoryTurns, MaxTokens: defaultMaxHistoryTokens}
}

// TrimHistory 按上限裁剪历史，返回新的切片；未发生裁剪时原样返回入参。
//
// 两条规则：
//   - 轮数：只保留最近 MaxTurns 轮，起点落在一条 user 消息上；
//   - 预算：按 token 估算从最旧的一轮开始整轮丢弃，不会把一轮切成两半；
//     若只剩最后一轮仍超预算，则保留它——总得让模型看见用户刚说的话。
//
// 历史里只会有 user / assistant 两种消息（工具调用的中间轮次不出现在历史中），
// 因此裁剪不需要处理「孤立的工具结果」这种结构问题。
func TrimHistory(history []llm.Message, limits HistoryLimits) []llm.Message {
	start := 0

	if limits.MaxTurns > 0 {
		seen := 0
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role != llm.RoleUser {
				continue
			}
			seen++
			if seen == limits.MaxTurns {
				start = i
				break
			}
		}
	}

	if limits.MaxTokens > 0 {
		for start < len(history) && estimateMessages(history[start:]) > limits.MaxTokens {
			next := nextUserIndex(history, start+1)
			if next < 0 {
				break
			}
			start = next
		}
	}

	if start == 0 {
		return history
	}

	out := make([]llm.Message, len(history)-start)
	copy(out, history[start:])
	return out
}

// nextUserIndex 返回 from 起第一条 user 消息的下标，没有则返回 -1。
func nextUserIndex(history []llm.Message, from int) int {
	for i := from; i < len(history); i++ {
		if history[i].Role == llm.RoleUser {
			return i
		}
	}
	return -1
}

// estimateMessages 估算一批消息的 token 总量。
func estimateMessages(messages []llm.Message) int {
	total := 0
	for _, message := range messages {
		total += estimateTokens(message.Content)
	}
	return total
}

// estimateTokens 估算一段文本的 token 数。
//
// 没有引入分词器（保持纯 Go、零依赖），按字符类别保守估算：
// 中日韩字符按 1 字 ≈ 1 token，其余字符按 4 字 ≈ 1 token。
// 估高只会让裁剪更早发生，不会把请求顶出模型上下文窗口。
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}

	cjk, other := 0, 0
	for _, r := range text {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}

	return cjk + (other+3)/4
}

// isCJK 判断是否中日韩文字或全角标点。
func isCJK(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F: // CJK 标点
		return true
	case r >= 0x3040 && r <= 0x30FF: // 日文假名
		return true
	case r >= 0x3400 && r <= 0x4DBF: // 扩展 A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // 统一表意文字
		return true
	case r >= 0xF900 && r <= 0xFAFF: // 兼容表意文字
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // 全角字符
		return true
	default:
		return false
	}
}
