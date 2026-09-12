package app

import (
	"context"
	"fmt"
	"strconv"

	"onebot-gateway/gateway/distillery"
	"onebot-gateway/gateway/event/bilibililive"
	onebot "onebot-gateway/gateway/event/onebot"
	"onebot-gateway/gateway/upload"
	"onebot-gateway/shared/action"
)

func Register() {
	registerQQ()
	registerBilibili()
}

const (
	platformQQ       = "qq"
	platformBilibili = "bilibili"
)

func registerQQ() {
	onebot.MessageGroupActions.Add(func(ctx context.Context, e onebot.MessageGroupEvent) action.Action {
		upload.Upload(ctx, platformQQ, e)
		return action.Action{}
	})

	registerNotice(&onebot.NoticeGroupUploadActions, platformQQ, func(e onebot.NoticeGroupUploadEvent) noticeRecord {
		return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 在群 %d 上传了文件《%s》", e.UserID, e.GroupID, e.File.Name)}
	})
	registerNotice(&onebot.NoticeGroupAdminActions, platformQQ, func(e onebot.NoticeGroupAdminEvent) noticeRecord {
		actionText := "被设置为管理员"
		if e.SubType == "unset" {
			actionText = "被取消了管理员"
		}
		return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 在群 %d %s", e.UserID, e.GroupID, actionText)}
	})
	registerNotice(&onebot.NoticeGroupDecreaseActions, platformQQ, func(e onebot.NoticeGroupDecreaseEvent) noticeRecord {
		if e.SubType == "leave" {
			return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 退出了群 %d", e.UserID, e.GroupID)}
		}
		return noticeRecord{e.OperatorID, fmt.Sprintf("QQ %d 将 QQ %d 移出了群 %d", e.OperatorID, e.UserID, e.GroupID)}
	})
	registerNotice(&onebot.NoticeGroupIncreaseActions, platformQQ, func(e onebot.NoticeGroupIncreaseEvent) noticeRecord {
		if e.SubType == "invite" {
			return noticeRecord{e.OperatorID, fmt.Sprintf("QQ %d 邀请 QQ %d 加入了群 %d", e.OperatorID, e.UserID, e.GroupID)}
		}
		return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 加入了群 %d", e.UserID, e.GroupID)}
	})
	registerNotice(&onebot.NoticeGroupBanActions, platformQQ, func(e onebot.NoticeGroupBanEvent) noticeRecord {
		if e.SubType == "lift_ban" {
			return noticeRecord{e.OperatorID, fmt.Sprintf("QQ %d 解除了 QQ %d 在群 %d 的禁言", e.OperatorID, e.UserID, e.GroupID)}
		}
		return noticeRecord{e.OperatorID, fmt.Sprintf("QQ %d 将 QQ %d 在群 %d 禁言了 %d 秒", e.OperatorID, e.UserID, e.GroupID, e.Duration)}
	})
	registerNotice(&onebot.NoticeFriendAddActions, platformQQ, func(e onebot.NoticeFriendAddEvent) noticeRecord {
		return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 添加了机器人为好友", e.UserID)}
	})
	registerNotice(&onebot.NoticeGroupRecallActions, platformQQ, func(e onebot.NoticeGroupRecallEvent) noticeRecord {
		return noticeRecord{e.OperatorID, fmt.Sprintf("QQ %d 撤回了 QQ %d 在群 %d 的消息 %d", e.OperatorID, e.UserID, e.GroupID, e.MessageID)}
	})
	registerNotice(&onebot.NoticeFriendRecallActions, platformQQ, func(e onebot.NoticeFriendRecallEvent) noticeRecord {
		return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 撤回了消息 %d", e.UserID, e.MessageID)}
	})
	registerNotice(&onebot.NoticeNotifyPokeActions, platformQQ, func(e onebot.NoticeNotifyPokeEvent) noticeRecord {
		return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 在群 %d 戳了 QQ %d", e.UserID, e.GroupID, e.TargetID)}
	})
	registerNotice(&onebot.NoticeNotifyLuckyKingActions, platformQQ, func(e onebot.NoticeNotifyLuckyKingEvent) noticeRecord {
		return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 在群 %d 发送了红包，QQ %d 成为了运气王", e.UserID, e.GroupID, e.TargetID)}
	})
	registerNotice(&onebot.NoticeNotifyHonorActions, platformQQ, func(e onebot.NoticeNotifyHonorEvent) noticeRecord {
		honor := map[string]string{
			"talkative": "龙王",
			"performer": "群聊之火",
			"emotion":   "快乐源泉",
		}[e.HonorType]
		if honor == "" {
			honor = e.HonorType
		}
		return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 在群 %d 获得了%s荣誉", e.UserID, e.GroupID, honor)}
	})
	registerNotice(&onebot.NoticeNotifyInputStatusActions, platformQQ, func(e onebot.NoticeNotifyInputStatusEvent) noticeRecord {
		return noticeRecord{e.UserID, fmt.Sprintf("QQ %d 的输入状态变为%s", e.UserID, e.StatusText)}
	})
}

func registerBilibili() {
	bilibililive.LiveOpenPlatformDMActions.Add(func(ctx context.Context, e bilibililive.LiveOpenPlatformDMEvent) action.Action {
		upload.UploadBilibiliDM(ctx, platformBilibili, e)
		return action.Action{}
	})

	bilibililive.LiveOpenPlatformSendGiftActions.Add(func(ctx context.Context, e bilibililive.LiveOpenPlatformSendGiftEvent) action.Action {
		upload.UploadBilibiliGift(ctx, platformBilibili, e)
		distillery.Post(buildBilibiliGiftDistilleryEvent(e))
		return action.Action{}
	})

	bilibililive.LiveOpenPlatformSuperChatActions.Add(func(ctx context.Context, e bilibililive.LiveOpenPlatformSuperChatEvent) action.Action {
		upload.UploadBilibiliSuperChat(ctx, platformBilibili, e)
		distillery.Post(buildBilibiliSuperChatDistilleryEvent(e))
		return action.Action{}
	})

	bilibililive.LiveOpenPlatformGuardActions.Add(func(ctx context.Context, e bilibililive.LiveOpenPlatformGuardEvent) action.Action {
		upload.UploadBilibiliGuard(ctx, platformBilibili, e)
		distillery.Post(buildBilibiliGuardDistilleryEvent(e))
		return action.Action{}
	})

	bilibililive.LiveOpenPlatformLikeActions.Add(func(ctx context.Context, e bilibililive.LiveOpenPlatformLikeEvent) action.Action {
		upload.UploadBilibiliNotice(
			ctx,
			platformBilibili,
			bilibiliIdentity(e.OpenID, e.UID),
			e.UName,
			buildBilibiliLikeNoticeText(e),
		)
		return action.Action{}
	})

	bilibililive.LiveOpenPlatformLiveRoomEnterActions.Add(func(ctx context.Context, e bilibililive.LiveOpenPlatformLiveRoomEnterEvent) action.Action {
		upload.UploadBilibiliNotice(
			ctx,
			platformBilibili,
			bilibiliIdentity(e.OpenID, e.UID),
			e.UName,
			buildBilibiliLiveRoomEnterNoticeText(e),
		)
		return action.Action{}
	})

	bilibililive.LiveOpenPlatformLiveStartActions.Add(func(ctx context.Context, e bilibililive.LiveOpenPlatformLiveStartEvent) action.Action {
		upload.UploadBilibiliNotice(ctx, platformBilibili, e.OpenID, e.OpenID, "[开播]")
		return action.Action{}
	})

	bilibililive.LiveOpenPlatformLiveEndActions.Add(func(ctx context.Context, e bilibililive.LiveOpenPlatformLiveEndEvent) action.Action {
		upload.UploadBilibiliNotice(ctx, platformBilibili, e.OpenID, e.OpenID, "[下播]")
		return action.Action{}
	})
}

func buildBilibiliGiftDistilleryEvent(e bilibililive.LiveOpenPlatformSendGiftEvent) distillery.UnifiedEvent {
	return buildBilibiliDistilleryEvent(
		bilibiliIdentity(e.OpenID, e.UID),
		e.UName,
		e.RoomID,
		fmt.Sprintf("%s×%d", e.GiftName, e.GiftNum),
		"gift",
	)
}

func buildBilibiliSuperChatDistilleryEvent(e bilibililive.LiveOpenPlatformSuperChatEvent) distillery.UnifiedEvent {
	return buildBilibiliDistilleryEvent(
		bilibiliIdentity(e.OpenID, e.UID),
		e.UName,
		e.RoomID,
		e.Message,
		"super_chat",
	)
}

func buildBilibiliGuardDistilleryEvent(e bilibililive.LiveOpenPlatformGuardEvent) distillery.UnifiedEvent {
	var openID, uname string
	var uid int64
	if e.UserInfo != nil {
		openID = e.UserInfo.OpenID
		uid = e.UserInfo.UID
		uname = e.UserInfo.UName
	}
	return buildBilibiliDistilleryEvent(
		bilibiliIdentity(openID, uid),
		uname,
		e.RoomID,
		fmt.Sprintf("开通舰长×%d%s", e.GuardNum, e.GuardUnit),
		"captain",
	)
}

func buildBilibiliDistilleryEvent(userID, userName string, roomID int64, content, messageType string) distillery.UnifiedEvent {
	if userName == "" {
		userName = userID
	}
	return distillery.UnifiedEvent{
		Platform:    platformBilibili,
		UserID:      userID,
		UserName:    userName,
		GroupID:     fmt.Sprintf("room_%d", roomID),
		Content:     content,
		MessageType: messageType,
	}
}

func buildBilibiliLikeNoticeText(e bilibililive.LiveOpenPlatformLikeEvent) string {
	return fmt.Sprintf("[点赞] %s", e.LikeText)
}

func buildBilibiliLiveRoomEnterNoticeText(e bilibililive.LiveOpenPlatformLiveRoomEnterEvent) string {
	return fmt.Sprintf("[进入直播间] %s", e.UName)
}

func bilibiliIdentity(openID string, uid int64) string {
	if openID != "" {
		return openID
	}
	if uid != 0 {
		return strconv.FormatInt(uid, 10)
	}
	return ""
}

type noticeRecord struct {
	userID int64
	text   string
}

func registerNotice[T any](actions *onebot.ActionList[T], platform string, record func(T) noticeRecord) {
	actions.Add(func(ctx context.Context, e T) action.Action {
		notice := record(e)
		upload.UploadNotice(ctx, platform, notice.userID, notice.text)
		return action.Action{}
	})
}
