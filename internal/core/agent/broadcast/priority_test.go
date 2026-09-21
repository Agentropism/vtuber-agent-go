package broadcast

import (
	"testing"

	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/event"
)

func TestPriorityFor(t *testing.T) {
	cases := []struct {
		kind event.Kind
		want Priority
	}{
		{event.KindSuperChat, PrioritySuperChat},
		{event.KindGift, PriorityGift},
		{event.KindGuard, PriorityGift},
		{event.KindDanmaku, PriorityDanmaku},
		{event.KindGroupMessage, PriorityDanmaku},
		// 未声明的种类按弹幕处理，不静默降级
		{event.Kind("未来新增的种类"), PriorityDanmaku},
		{event.Kind(""), PriorityDanmaku},
	}

	for _, tc := range cases {
		if got := PriorityFor(tc.kind); got != tc.want {
			t.Errorf("种类 %q 的优先级为 %s，期望 %s", tc.kind, got, tc.want)
		}
	}
}

// TestPriorityOrderMatchesPlan 锁定计划里的分层顺序：SC > 礼物 > 弹幕 > 主动 > 待机。
func TestPriorityOrderMatchesPlan(t *testing.T) {
	order := []Priority{
		PrioritySuperChat,
		PriorityGift,
		PriorityDanmaku,
		PriorityProactive,
		PriorityIdle,
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] <= order[i] {
			t.Fatalf("优先级顺序不符合计划: %s 应高于 %s", order[i-1], order[i])
		}
	}
}
