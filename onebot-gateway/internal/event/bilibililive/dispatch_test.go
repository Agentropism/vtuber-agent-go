package bilibililive

import (
	"context"
	"strconv"
	"testing"

	"onebot-gateway/internal/action"
)

func TestDispatchRoutesSupportedEvents(t *testing.T) {
	LiveOpenPlatformDMActions.Add(func(ctx context.Context, e LiveOpenPlatformDMEvent) action.Action {
		return action.Action{Action: e.Message}
	})
	LiveOpenPlatformDMMirrorActions.Add(func(ctx context.Context, e LiveOpenPlatformDMMirrorEvent) action.Action {
		return action.Action{Action: e.Message}
	})
	LiveOpenPlatformSendGiftActions.Add(func(ctx context.Context, e LiveOpenPlatformSendGiftEvent) action.Action {
		return action.Action{Action: e.GiftName}
	})
	LiveOpenPlatformSuperChatActions.Add(func(ctx context.Context, e LiveOpenPlatformSuperChatEvent) action.Action {
		return action.Action{Action: e.Message}
	})
	LiveOpenPlatformSuperChatDelActions.Add(func(ctx context.Context, e LiveOpenPlatformSuperChatDelEvent) action.Action {
		return action.Action{Action: strconv.Itoa(len(e.MessageIDs))}
	})
	LiveOpenPlatformGuardActions.Add(func(ctx context.Context, e LiveOpenPlatformGuardEvent) action.Action {
		return action.Action{Action: strconv.FormatInt(e.GuardLevel, 10)}
	})
	LiveOpenPlatformLikeActions.Add(func(ctx context.Context, e LiveOpenPlatformLikeEvent) action.Action {
		return action.Action{Action: e.LikeText}
	})
	LiveOpenPlatformLiveRoomEnterActions.Add(func(ctx context.Context, e LiveOpenPlatformLiveRoomEnterEvent) action.Action {
		return action.Action{Action: e.UName}
	})
	LiveOpenPlatformLiveStartActions.Add(func(ctx context.Context, e LiveOpenPlatformLiveStartEvent) action.Action {
		return action.Action{Action: e.OpenID}
	})
	LiveOpenPlatformLiveEndActions.Add(func(ctx context.Context, e LiveOpenPlatformLiveEndEvent) action.Action {
		return action.Action{Action: e.OpenID}
	})
	LiveOpenPlatformInteractionEndActions.Add(func(ctx context.Context, e LiveOpenPlatformInteractionEndEvent) action.Action {
		return action.Action{Action: e.GameID}
	})

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"弹幕", `{"cmd":"LIVE_OPEN_PLATFORM_DM","data":{"msg":"你好"}}`, "你好"},
		{"弹幕镜像", `{"cmd":"LIVE_OPEN_PLATFORM_DM_MIRROR","data":{"msg":"镜像"}}`, "镜像"},
		{"礼物", `{"cmd":"LIVE_OPEN_PLATFORM_SEND_GIFT","data":{"gift_name":"火箭"}}`, "火箭"},
		{"醒目留言", `{"cmd":"LIVE_OPEN_PLATFORM_SUPER_CHAT","data":{"message":"加油"}}`, "加油"},
		{"醒目留言撤回", `{"cmd":"LIVE_OPEN_PLATFORM_SUPER_CHAT_DEL","data":{"message_ids":[1,2]}}`, "2"},
		{"大航海", `{"cmd":"LIVE_OPEN_PLATFORM_GUARD","data":{"guard_level":3}}`, "3"},
		{"点赞", `{"cmd":"LIVE_OPEN_PLATFORM_LIKE","data":{"like_text":"点赞了"}}`, "点赞了"},
		{"进入直播间", `{"cmd":"LIVE_OPEN_PLATFORM_LIVE_ROOM_ENTER","data":{"uname":"观众"}}`, "观众"},
		{"开播", `{"cmd":"LIVE_OPEN_PLATFORM_LIVE_START","data":{"open_id":"主播"}}`, "主播"},
		{"下播", `{"cmd":"LIVE_OPEN_PLATFORM_LIVE_END","data":{"open_id":"主播"}}`, "主播"},
		{"互动结束", `{"cmd":"LIVE_OPEN_PLATFORM_INTERACTION_END","data":{"game_id":"game_1"}}`, "game_1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Dispatch(context.Background(), []byte(tt.raw))
			if got.Action != tt.want {
				t.Fatalf("分发结果错误: got=%q want=%q", got.Action, tt.want)
			}
		})
	}
}
