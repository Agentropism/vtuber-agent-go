package broadcast

import "github.com/Agentropism/vtuber-agent-go/internal/shared/event"

// PriorityFor 把事件种类映射成播报优先级。
//
// 与计划的优先级分层一一对应：SC > 礼物/大航海 > 弹幕与群消息。
// PriorityProactive 与 PriorityIdle 不由事件触发，留给后续的主动互动与待机发言。
//
// 未声明的种类按弹幕处理，避免新种类因为忘了登记而被静默降到最低优先级。
func PriorityFor(kind event.Kind) Priority {
	switch kind {
	case event.KindSuperChat:
		return PrioritySuperChat
	case event.KindGift, event.KindGuard:
		return PriorityGift
	default:
		return PriorityDanmaku
	}
}
