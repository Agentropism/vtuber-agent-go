package event

import (
	"context"

	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/event/bilibililive"
	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/event/onebot"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/action"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

const (
	PlatformQQ       = "qq"
	PlatformBilibili = "bilibili"
)

func Dispatch(ctx context.Context, platform string, raw []byte) action.Action {
	switch platform {
	case PlatformQQ:
		return onebot.Dispatch(ctx, raw)
	case PlatformBilibili:
		return bilibililive.Dispatch(ctx, raw)
	default:
		logger.Debugf("未知平台 platform=%s", platform)
		return action.Action{}
	}
}
