package onebot

import (
	"context"
	"encoding/json"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/action"
)

type eventTypeProbe struct {
	PostType      string `json:"post_type"`
	MetaEventType string `json:"meta_event_type"`
	NoticeType    string `json:"notice_type"`
	MessageType   string `json:"message_type"`
	RequestType   string `json:"request_type"`
	SubType       string `json:"sub_type"`
}

func Dispatch(ctx context.Context, raw []byte) action.Action {
	var probe eventTypeProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		logger.Warnf("事件类型探测失败: %v", err)
		return action.Action{}
	}

	switch probe.PostType {
	case "meta_event":
		return dispatchMetaEvent(ctx, probe.MetaEventType, raw)
	case "notice":
		return dispatchNoticeEvent(ctx, probe.NoticeType, probe.SubType, raw)
	case "message":
		return dispatchMessageEvent(ctx, probe.MessageType, raw)
	case "request":
		return dispatchRequestEvent(ctx, probe.RequestType, raw)
	default:
		logger.Debugf("未知事件类型 post_type=%s", probe.PostType)
		return action.Action{}
	}
}

func dispatchMetaEvent(ctx context.Context, metaEventType string, raw []byte) action.Action {
	switch metaEventType {
	case "lifecycle":
		var event MetaLifecycleEvent
		return decodeAndDispatch(ctx, raw, &event, "meta lifecycle", &MetaLifecycleActions)
	case "heartbeat":
		var event MetaHeartbeatEvent
		return decodeAndDispatch(ctx, raw, &event, "meta heartbeat", &MetaHeartbeatActions)
	default:
		logger.Debugf("未知元事件类型 meta_event_type=%s", metaEventType)
		return action.Action{}
	}
}

func dispatchMessageEvent(ctx context.Context, messageType string, raw []byte) action.Action {
	switch messageType {
	case "private":
		var event MessagePrivateEvent
		return decodeAndDispatch(ctx, raw, &event, "message private", &MessagePrivateActions)
	case "group":
		var event MessageGroupEvent
		return decodeAndDispatch(ctx, raw, &event, "message group", &MessageGroupActions)
	default:
		logger.Debugf("未知消息类型 message_type=%s", messageType)
		return action.Action{}
	}
}

func dispatchNoticeEvent(ctx context.Context, noticeType, subType string, raw []byte) action.Action {
	switch noticeType {
	case "group_upload":
		var event NoticeGroupUploadEvent
		return decodeAndDispatch(ctx, raw, &event, "notice group_upload", &NoticeGroupUploadActions)
	case "group_admin":
		var event NoticeGroupAdminEvent
		return decodeAndDispatch(ctx, raw, &event, "notice group_admin", &NoticeGroupAdminActions)
	case "group_decrease":
		var event NoticeGroupDecreaseEvent
		return decodeAndDispatch(ctx, raw, &event, "notice group_decrease", &NoticeGroupDecreaseActions)
	case "group_increase":
		var event NoticeGroupIncreaseEvent
		return decodeAndDispatch(ctx, raw, &event, "notice group_increase", &NoticeGroupIncreaseActions)
	case "group_ban":
		var event NoticeGroupBanEvent
		return decodeAndDispatch(ctx, raw, &event, "notice group_ban", &NoticeGroupBanActions)
	case "friend_add":
		var event NoticeFriendAddEvent
		return decodeAndDispatch(ctx, raw, &event, "notice friend_add", &NoticeFriendAddActions)
	case "group_recall":
		var event NoticeGroupRecallEvent
		return decodeAndDispatch(ctx, raw, &event, "notice group_recall", &NoticeGroupRecallActions)
	case "friend_recall":
		var event NoticeFriendRecallEvent
		return decodeAndDispatch(ctx, raw, &event, "notice friend_recall", &NoticeFriendRecallActions)
	case "notify":
		return dispatchNotifyEvent(ctx, subType, raw)
	default:
		logger.Debugf("未知通知类型 notice_type=%s", noticeType)
		return action.Action{}
	}
}

func dispatchNotifyEvent(ctx context.Context, subType string, raw []byte) action.Action {
	switch subType {
	case "poke":
		var event NoticeNotifyPokeEvent
		return decodeAndDispatch(ctx, raw, &event, "notice notify poke", &NoticeNotifyPokeActions)
	case "lucky_king":
		var event NoticeNotifyLuckyKingEvent
		return decodeAndDispatch(ctx, raw, &event, "notice notify lucky_king", &NoticeNotifyLuckyKingActions)
	case "honor":
		var event NoticeNotifyHonorEvent
		return decodeAndDispatch(ctx, raw, &event, "notice notify honor", &NoticeNotifyHonorActions)
	case "input_status":
		var event NoticeNotifyInputStatusEvent
		return decodeAndDispatch(ctx, raw, &event, "notice notify input_status", &NoticeNotifyInputStatusActions)
	default:
		logger.Debugf("未知通知子类型 sub_type=%s", subType)
		return action.Action{}
	}
}

func dispatchRequestEvent(ctx context.Context, requestType string, raw []byte) action.Action {
	switch requestType {
	case "friend":
		var event RequestFriendEvent
		return decodeAndDispatch(ctx, raw, &event, "request friend", &RequestFriendActions)
	case "group":
		var event RequestGroupEvent
		return decodeAndDispatch(ctx, raw, &event, "request group", &RequestGroupActions)
	default:
		logger.Debugf("未知请求类型 request_type=%s", requestType)
		return action.Action{}
	}
}

func decodeAndDispatch[T any](
	ctx context.Context,
	raw []byte,
	event *T,
	eventType string,
	handlers *ActionList[T],
) action.Action {
	if err := json.Unmarshal(raw, event); err != nil {
		return decodeFailed(eventType, err)
	}
	return DispatchWithHandlers(ctx, *event, handlers)
}

func decodeFailed(eventType string, err error) action.Action {
	logger.Warnf("解析 %s 事件失败: %v", eventType, err)
	return action.Action{}
}
