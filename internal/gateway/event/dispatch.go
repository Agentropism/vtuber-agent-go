package event

import (
	"context"

	"github.com/Agentropism/vtuber-agent-go/internal/gateway/event/bilibililive"
	"github.com/Agentropism/vtuber-agent-go/internal/gateway/event/onebot"
	"github.com/Agentropism/vtuber-agent-go/internal/shared/action"

	"go.uber.org/zap"
)

const (
	PlatformQQ       = "qq"
	PlatformBilibili = "bilibili"
)

var log = zap.NewNop()

func SetLogger(logger *zap.Logger) {
	if logger != nil {
		log = logger
	}
	onebot.SetLogger(logger)
	bilibililive.SetLogger(logger)
}

func Dispatch(ctx context.Context, platform string, raw []byte) action.Action {
	switch platform {
	case PlatformQQ:
		return onebot.Dispatch(ctx, raw)
	case PlatformBilibili:
		return bilibililive.Dispatch(ctx, raw)
	default:
		log.Sugar().Debugf("未知平台 platform=%s", platform)
		return action.Action{}
	}
}
