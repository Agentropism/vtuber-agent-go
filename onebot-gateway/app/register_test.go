package app

import (
	"testing"

	"onebot-gateway/gateway/event/bilibililive"
)

func TestBuildBilibiliGiftDistilleryEvent(t *testing.T) {
	e := bilibililive.LiveOpenPlatformSendGiftEvent{
		RoomID:   123456,
		OpenID:   "open_gift",
		UName:    "送礼用户",
		GiftName: "火箭",
		GiftNum:  2,
	}

	got := buildBilibiliGiftDistilleryEvent(e)
	if got.Platform != platformBilibili || got.GroupID != "room_123456" {
		t.Fatalf("礼物事件平台字段错误: %#v", got)
	}
	if got.UserID != "open_gift" || got.UserName != "送礼用户" {
		t.Fatalf("礼物事件用户字段错误: %#v", got)
	}
	if got.MessageType != "gift" || got.Content != "火箭×2" {
		t.Fatalf("礼物事件内容错误: %#v", got)
	}
}

func TestBuildBilibiliSuperChatDistilleryEvent(t *testing.T) {
	e := bilibililive.LiveOpenPlatformSuperChatEvent{
		RoomID:  123456,
		OpenID:  "open_sc",
		UName:   "SC用户",
		Message: "主播加油！",
	}

	got := buildBilibiliSuperChatDistilleryEvent(e)
	if got.MessageType != "super_chat" || got.Content != "主播加油！" {
		t.Fatalf("醒目留言事件内容错误: %#v", got)
	}
}

func TestBuildBilibiliGuardDistilleryEvent(t *testing.T) {
	e := bilibililive.LiveOpenPlatformGuardEvent{
		UserInfo: &bilibililive.LiveOpenPlatformGuardUserInfo{
			OpenID: "open_guard",
			UName:  "上舰用户",
		},
		GuardNum:  1,
		GuardUnit: "月",
		RoomID:    123456,
	}

	got := buildBilibiliGuardDistilleryEvent(e)
	if got.UserID != "open_guard" || got.UserName != "上舰用户" {
		t.Fatalf("大航海事件用户字段错误: %#v", got)
	}
	if got.MessageType != "captain" || got.Content != "开通舰长×1月" {
		t.Fatalf("大航海事件内容错误: %#v", got)
	}
}

func TestBuildBilibiliNoticeText(t *testing.T) {
	like := buildBilibiliLikeNoticeText(bilibililive.LiveOpenPlatformLikeEvent{
		LikeText: "点赞用户点赞了",
	})
	if like != "[点赞] 点赞用户点赞了" {
		t.Fatalf("点赞通知文本错误: %q", like)
	}

	enter := buildBilibiliLiveRoomEnterNoticeText(bilibililive.LiveOpenPlatformLiveRoomEnterEvent{
		UName: "进入用户",
	})
	if enter != "[进入直播间] 进入用户" {
		t.Fatalf("进入直播间通知文本错误: %q", enter)
	}
}
