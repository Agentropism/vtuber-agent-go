package bilibililive

import (
	"context"
	"fmt"

	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/action"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

type ActionList[T any] struct {
	handlers []func(context.Context, T) action.Action
}

func newActionList[T any](eventType string) ActionList[T] {
	var list ActionList[T]
	list.Add(func(ctx context.Context, event T) action.Action {
		logger.Debugf("分发 %s 事件: %s", eventType, fmt.Sprint(event))
		return action.Action{}
	})
	return list
}

var (
	LiveOpenPlatformDMActions             = newActionList[LiveOpenPlatformDMEvent]("B站弹幕")
	LiveOpenPlatformDMMirrorActions       = newActionList[LiveOpenPlatformDMMirrorEvent]("B站弹幕镜像")
	LiveOpenPlatformSendGiftActions       = newActionList[LiveOpenPlatformSendGiftEvent]("B站礼物")
	LiveOpenPlatformSuperChatActions      = newActionList[LiveOpenPlatformSuperChatEvent]("B站醒目留言")
	LiveOpenPlatformSuperChatDelActions   = newActionList[LiveOpenPlatformSuperChatDelEvent]("B站醒目留言撤回")
	LiveOpenPlatformGuardActions          = newActionList[LiveOpenPlatformGuardEvent]("B站大航海")
	LiveOpenPlatformLikeActions           = newActionList[LiveOpenPlatformLikeEvent]("B站点赞")
	LiveOpenPlatformLiveRoomEnterActions  = newActionList[LiveOpenPlatformLiveRoomEnterEvent]("B站进入直播间")
	LiveOpenPlatformLiveStartActions      = newActionList[LiveOpenPlatformLiveStartEvent]("B站开播")
	LiveOpenPlatformLiveEndActions        = newActionList[LiveOpenPlatformLiveEndEvent]("B站下播")
	LiveOpenPlatformInteractionEndActions = newActionList[LiveOpenPlatformInteractionEndEvent]("B站互动结束")
)

func (l *ActionList[T]) Add(handlers ...func(context.Context, T) action.Action) {
	l.handlers = append(l.handlers, handlers...)
}

func (l *ActionList[T]) Run(ctx context.Context, event T) action.Action {
	var result action.Action
	if l == nil {
		return result
	}
	for _, fn := range l.handlers {
		next := fn(ctx, event)
		if !isZeroAction(next) {
			result = next
		}
	}
	return result
}

func DispatchWithHandlers[T any](ctx context.Context, event T, handlers *ActionList[T]) action.Action {
	return handlers.Run(ctx, event)
}

func isZeroAction(next action.Action) bool {
	return next.Action == "" && next.Params == nil && next.Echo == nil
}
