package upload

import (
	"testing"

	"onebot-gateway/gateway/event/bilibililive"
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
	if got.Type != EventTypeMessage || got.ChannelID != "room_123456" {
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
}

func TestBuildBilibiliNoticeUsesStringIdentity(t *testing.T) {
	got := buildBilibiliNoticePlatformEvent(
		"bilibili",
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
