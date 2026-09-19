package upload

import (
	"testing"

	"github.com/Agentropism/vtuber-agent-go/internal/gateway/event/bilibililive"
	onebot "github.com/Agentropism/vtuber-agent-go/internal/gateway/event/onebot"
	"github.com/Agentropism/vtuber-agent-go/internal/shared/event"
)

func TestBuildBilibiliGiftPlatformEvent(t *testing.T) {
	e := bilibililive.LiveOpenPlatformSendGiftEvent{
		RoomID:    123456,
		OpenID:    "open_gift",
		UName:     "送礼用户",
		UFace:     "face.png",
		GiftName:  "火箭",
		GiftNum:   2,
		RPrice:    500000,
		Paid:      true,
		Timestamp: 1785800000,
		MessageID: "gift_msg",
	}

	got := buildBilibiliGiftPlatformEvent("bilibili", e)
	if got.Type != EventTypeMessage || got.Kind != event.KindGift {
		t.Fatalf("礼物事件类型字段错误: type=%q kind=%q", got.Type, got.Kind)
	}
	if got.ChannelID != "room_123456" {
		t.Fatalf("礼物事件路由字段错误: %#v", got)
	}
	if got.UserID != "open_gift" || got.MessageID != "gift_msg" {
		t.Fatalf("礼物事件身份字段错误: %#v", got)
	}
	if got.ContentText != "[礼物] 赠送 火箭×2（价值 1000 元）" {
		t.Fatalf("礼物文本错误: %q", got.ContentText)
	}
}

func TestBuildBilibiliFreeGiftPlatformEvent(t *testing.T) {
	e := bilibililive.LiveOpenPlatformSendGiftEvent{
		GiftName: "小心心",
		GiftNum:  5,
		Paid:     false,
	}

	got := buildBilibiliGiftPlatformEvent("bilibili", e)
	if got.ContentText != "[礼物] 赠送 小心心×5（免费礼物）" {
		t.Fatalf("免费礼物文本错误: %q", got.ContentText)
	}
	if got.Kind != event.KindGift {
		t.Fatalf("免费礼物种类错误: %q", got.Kind)
	}
}

func TestBuildBilibiliSuperChatPlatformEvent(t *testing.T) {
	e := bilibililive.LiveOpenPlatformSuperChatEvent{
		RoomID:  123456,
		OpenID:  "open_sc",
		UName:   "SC用户",
		Message: "主播加油！",
		MsgID:   "sc_msg",
		RMB:     30,
	}

	got := buildBilibiliSuperChatPlatformEvent("bilibili", e)
	if got.ContentText != "[醒目留言 30元] 主播加油！" {
		t.Fatalf("醒目留言文本错误: %q", got.ContentText)
	}
	if got.UserID != "open_sc" || got.MessageID != "sc_msg" {
		t.Fatalf("醒目留言身份字段错误: %#v", got)
	}
	if got.Kind != event.KindSuperChat {
		t.Fatalf("醒目留言种类错误: %q", got.Kind)
	}
}

func TestBuildBilibiliGuardPlatformEvent(t *testing.T) {
	e := bilibililive.LiveOpenPlatformGuardEvent{
		UserInfo: &bilibililive.LiveOpenPlatformGuardUserInfo{
			OpenID: "open_guard",
			UName:  "上舰用户",
		},
		GuardLevel: 3,
		GuardNum:   1,
		GuardUnit:  "月",
		RoomID:     123456,
		MessageID:  "guard_msg",
	}

	got := buildBilibiliGuardPlatformEvent("bilibili", e)
	if got.ContentText != "[大航海] 开通舰长×1月" {
		t.Fatalf("大航海文本错误: %q", got.ContentText)
	}
	if got.UserID != "open_guard" || got.UserName != "上舰用户" {
		t.Fatalf("大航海身份字段错误: %#v", got)
	}
	if got.Kind != event.KindGuard {
		t.Fatalf("大航海种类错误: %q", got.Kind)
	}
}

func TestBuildBilibiliDanmakuPlatformEvent(t *testing.T) {
	e := bilibililive.LiveOpenPlatformDMEvent{
		RoomID:      123456,
		OpenID:      "open_dm",
		UName:       "弹幕用户",
		Message:     "主播好",
		MessageID:   "dm_msg",
		ReplyOpenID: "open_other",
	}

	got := buildBilibiliDMPlatformEvent("bilibili", e)
	if got.Kind != event.KindDanmaku || got.Type != EventTypeMessage {
		t.Fatalf("弹幕种类错误: type=%q kind=%q", got.Type, got.Kind)
	}
	if got.ChannelID != "room_123456" || got.ChannelType != "live_room" {
		t.Fatalf("弹幕路由字段错误: %#v", got)
	}
	if got.RefSenderID != "open_other" {
		t.Fatalf("弹幕引用字段错误: %q", got.RefSenderID)
	}
}

// TestBuildBilibiliNoticeKinds 覆盖四类通知各自的种类，它们共用同一个构造器。
func TestBuildBilibiliNoticeKinds(t *testing.T) {
	cases := []struct {
		name string
		kind event.Kind
	}{
		{"点赞", event.KindLike},
		{"进入直播间", event.KindLiveRoomEnter},
		{"开播", event.KindLiveStart},
		{"下播", event.KindLiveEnd},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildBilibiliNoticePlatformEvent("bilibili", tc.kind, "open_user", "用户", "文本")
			if got.Kind != tc.kind {
				t.Fatalf("种类错误: %q", got.Kind)
			}
			if got.Type != EventTypeNotice {
				t.Fatalf("通知的粗粒度 type 应为 notice: %q", got.Type)
			}
		})
	}
}

func TestBuildBilibiliNoticeUsesStringIdentity(t *testing.T) {
	got := buildBilibiliNoticePlatformEvent(
		"bilibili",
		event.KindLike,
		"open_like",
		"点赞用户",
		"[点赞] 点赞用户点赞了",
	)

	if got.Type != EventTypeNotice {
		t.Fatalf("通知事件类型错误: %q", got.Type)
	}
	if got.UserID != "open_like" || got.SenderID != "open_like" {
		t.Fatalf("通知字符串身份错误: %#v", got)
	}
	if got.ContentText != "[点赞] 点赞用户点赞了" {
		t.Fatalf("通知文本错误: %q", got.ContentText)
	}
}

func TestBuildGroupMessagePlatformEvent(t *testing.T) {
	e := onebot.MessageGroupEvent{
		BaseEvent:  onebot.BaseEvent{Time: 1785800000, SelfID: 99999},
		UserID:     20002,
		GroupID:    10001,
		GroupName:  "技术交流群",
		MessageID:  30003,
		RawMessage: "大家好",
	}
	e.Sender.Card = "小明"
	e.Sender.Nickname = "明"

	got := buildPlatformEvent("qq", e)
	if got.Kind != event.KindGroupMessage || got.Type != EventTypeMessage {
		t.Fatalf("群消息种类错误: type=%q kind=%q", got.Type, got.Kind)
	}
	if got.ChannelID != "group_10001" || got.ChannelType != "group" {
		t.Fatalf("群消息路由字段错误: %#v", got)
	}
	if got.IsSelf {
		t.Fatal("群消息不应被标记为机器人自身发出")
	}
}

func TestBuildNoticePlatformEvent(t *testing.T) {
	got := buildNoticePlatformEvent("qq", 20002, "QQ 20002 加入了群 10001")
	if got.Kind != event.KindNotice || got.Type != EventTypeNotice {
		t.Fatalf("通知种类错误: type=%q kind=%q", got.Type, got.Kind)
	}
	if got.UserID != "20002" || got.ContentText != "QQ 20002 加入了群 10001" {
		t.Fatalf("通知字段错误: %#v", got)
	}
}
