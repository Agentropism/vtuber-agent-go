package event

import (
	"context"

	"onebot-gateway/internal/action"
	"onebot-gateway/internal/event/bilibililive"
	"onebot-gateway/internal/event/onebot"

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
