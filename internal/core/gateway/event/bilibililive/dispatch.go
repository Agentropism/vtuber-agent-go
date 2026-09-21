package bilibililive

import (
	"context"
	"encoding/json"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/action"
)

type eventTypeProbe struct {
	Cmd string `json:"cmd"`
}

func Dispatch(ctx context.Context, raw []byte) action.Action {
	var probe eventTypeProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		logger.Warnf("B站事件类型探测失败: %v", err)
		return action.Action{}
	}

	switch probe.Cmd {
	case CmdLiveOpenPlatformDM:
		var packet OpenPlatformPacket[LiveOpenPlatformDMEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站弹幕", &LiveOpenPlatformDMActions)
	case CmdLiveOpenPlatformDMMirror:
		var packet OpenPlatformPacket[LiveOpenPlatformDMMirrorEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站弹幕镜像", &LiveOpenPlatformDMMirrorActions)
	case CmdLiveOpenPlatformSendGift:
		var packet OpenPlatformPacket[LiveOpenPlatformSendGiftEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站礼物", &LiveOpenPlatformSendGiftActions)
	case CmdLiveOpenPlatformSuperChat:
		var packet OpenPlatformPacket[LiveOpenPlatformSuperChatEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站醒目留言", &LiveOpenPlatformSuperChatActions)
	case CmdLiveOpenPlatformSuperChatDel:
		var packet OpenPlatformPacket[LiveOpenPlatformSuperChatDelEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站醒目留言撤回", &LiveOpenPlatformSuperChatDelActions)
	case CmdLiveOpenPlatformGuard:
		var packet OpenPlatformPacket[LiveOpenPlatformGuardEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站大航海", &LiveOpenPlatformGuardActions)
	case CmdLiveOpenPlatformLike:
		var packet OpenPlatformPacket[LiveOpenPlatformLikeEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站点赞", &LiveOpenPlatformLikeActions)
	case CmdLiveOpenPlatformLiveRoomEnter:
		var packet OpenPlatformPacket[LiveOpenPlatformLiveRoomEnterEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站进入直播间", &LiveOpenPlatformLiveRoomEnterActions)
	case CmdLiveOpenPlatformLiveStart:
		var packet OpenPlatformPacket[LiveOpenPlatformLiveStartEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站开播", &LiveOpenPlatformLiveStartActions)
	case CmdLiveOpenPlatformLiveEnd:
		var packet OpenPlatformPacket[LiveOpenPlatformLiveEndEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站下播", &LiveOpenPlatformLiveEndActions)
	case CmdLiveOpenPlatformInteractionEnd:
		var packet OpenPlatformPacket[LiveOpenPlatformInteractionEndEvent]
		return decodeAndDispatch(ctx, raw, &packet, "B站互动结束", &LiveOpenPlatformInteractionEndActions)
	case CmdStatus:
		logger.Debugf("连接已发生")
		logger.Debug(string(raw)) // 纯消息、无占位符：走结构化形态，避免 vet 的格式串检查
		return action.Action{}
	default:
		logger.Debugf("未知B站事件类型 cmd=%s", probe.Cmd)
		return action.Action{}
	}
}

func decodeAndDispatch[T any](
	ctx context.Context,
	raw []byte,
	packet *OpenPlatformPacket[T],
	eventType string,
	handlers *ActionList[T],
) action.Action {
	if err := json.Unmarshal(raw, packet); err != nil {
		logger.Warnf("解析 %s 事件失败: %v", eventType, err)
		return action.Action{}
	}
	return DispatchWithHandlers(ctx, packet.Data, handlers)
}
