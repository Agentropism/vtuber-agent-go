// Package event 定义 platformEvent 的跨模块契约(执行计划 E1/E3)。
//
// 放在 shared 的原因:gateway/upload 负责产生事件、agent/conversation 负责消费事件,
// 两侧都要用到事件种类这个取值,而本包零内部依赖,不会引入方向问题。
//
// Kind 比 platformEvent 原有的 type 字段更细:type 只区分 message / notice,
// 而礼物、醒目留言、大航海在协议上全部属于 message,下游需要靠 kind 才能分辨,
// 进而决定播报优先级与是否需要 LLM 回复。
package event

// Kind 是 platformEvent 的事件种类。
type Kind string

// 事件种类取值。新增种类时同步更新 docs/EVENT_CONTRACT.md。
const (
	// QQ 侧
	KindGroupMessage Kind = "group_message" // 群消息
	KindNotice       Kind = "notice"        // 群通知(入群/禁言/撤回/戳一戳等)

	// B 站直播侧
	KindDanmaku       Kind = "danmaku"         // 弹幕
	KindSuperChat     Kind = "super_chat"      // 醒目留言
	KindGift          Kind = "gift"            // 礼物
	KindGuard         Kind = "guard"           // 大航海(舰长/提督/总督)
	KindLike          Kind = "like"            // 点赞
	KindLiveRoomEnter Kind = "live_room_enter" // 进入直播间
	KindLiveStart     Kind = "live_start"      // 开播
	KindLiveEnd       Kind = "live_end"        // 下播
)

// NeedsReply 表示该种类是否需要进入会话、由 LLM 生成回复。
//
// 礼物、醒目留言与大航海都需要被回应(致谢),因此与聊天消息同等对待;
// 点赞、进出直播间、开播下播与各类通知只做记录,不进会话。
func (k Kind) NeedsReply() bool {
	switch k {
	case KindGroupMessage, KindDanmaku, KindSuperChat, KindGift, KindGuard:
		return true
	default:
		return false
	}
}

// IsKnown 表示该种类是否在本包已声明。
func (k Kind) IsKnown() bool {
	switch k {
	case KindGroupMessage, KindNotice, KindDanmaku, KindSuperChat, KindGift,
		KindGuard, KindLike, KindLiveRoomEnter, KindLiveStart, KindLiveEnd:
		return true
	default:
		return false
	}
}

// String 返回种类的字符串形式。
func (k Kind) String() string { return string(k) }
