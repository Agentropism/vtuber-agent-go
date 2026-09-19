package onebot

import (
	"context"
	"fmt"

	"github.com/Agentropism/vtuber-agent-go/internal/shared/action"

	"go.uber.org/zap"
)

var log = zap.NewNop()

func SetLogger(logger *zap.Logger) {
	if logger != nil {
		log = logger
	}
}

type ActionList[T any] struct {
	handlers []func(context.Context, T) action.Action
}

func newActionList[T any](eventType string) ActionList[T] {
	var list ActionList[T]
	list.Add(func(ctx context.Context, event T) action.Action {
		log.Sugar().Debugf("分发 %s 事件: %s", eventType, fmt.Sprint(event))
		return action.Action{}
	})
	return list
}

var (
	MetaLifecycleActions = newActionList[MetaLifecycleEvent]("meta lifecycle")
	MetaHeartbeatActions = newActionList[MetaHeartbeatEvent]("meta heartbeat")

	MessagePrivateActions = newActionList[MessagePrivateEvent]("message private")
	MessageGroupActions   = newActionList[MessageGroupEvent]("message group")

	NoticeGroupUploadActions       = newActionList[NoticeGroupUploadEvent]("notice group_upload")
	NoticeGroupAdminActions        = newActionList[NoticeGroupAdminEvent]("notice group_admin")
	NoticeGroupDecreaseActions     = newActionList[NoticeGroupDecreaseEvent]("notice group_decrease")
	NoticeGroupIncreaseActions     = newActionList[NoticeGroupIncreaseEvent]("notice group_increase")
	NoticeGroupBanActions          = newActionList[NoticeGroupBanEvent]("notice group_ban")
	NoticeFriendAddActions         = newActionList[NoticeFriendAddEvent]("notice friend_add")
	NoticeGroupRecallActions       = newActionList[NoticeGroupRecallEvent]("notice group_recall")
	NoticeFriendRecallActions      = newActionList[NoticeFriendRecallEvent]("notice friend_recall")
	NoticeNotifyPokeActions        = newActionList[NoticeNotifyPokeEvent]("notice notify poke")
	NoticeNotifyLuckyKingActions   = newActionList[NoticeNotifyLuckyKingEvent]("notice notify lucky_king")
	NoticeNotifyHonorActions       = newActionList[NoticeNotifyHonorEvent]("notice notify honor")
	NoticeNotifyInputStatusActions = newActionList[NoticeNotifyInputStatusEvent]("notice notify input_status")

	RequestFriendActions = newActionList[RequestFriendEvent]("request friend")
	RequestGroupActions  = newActionList[RequestGroupEvent]("request group")
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
