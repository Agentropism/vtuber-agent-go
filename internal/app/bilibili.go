package app

import (
	"context"
	"errors"

	"github.com/Agentropism/vtuber-agent-go/internal/config"
	"github.com/Agentropism/vtuber-agent-go/internal/gateway/bilibili"
	"github.com/Agentropism/vtuber-agent-go/internal/gateway/event"
	"github.com/Agentropism/vtuber-agent-go/internal/gateway/upload"

	"go.uber.org/zap"
)

// provideBilibili 装配 B 站开放平台长连接。
//
// 凭据齐全时自动启用，也可以用 [bilibili].enabled 显式打开；都没配就返回 nil，
// 此时网关只服务 QQ 侧，B 站事件仍可由外部客户端经 WebSocket 接入。
func provideBilibili(cfg *config.Config, log *zap.Logger) (*bilibili.Client, error) {
	b := cfg.Bilibili
	complete := b.AccessKey != "" && b.AccessKeySecret != "" && b.IDCode != "" && b.AppID != 0

	if !b.Enabled && !complete {
		log.Sugar().Info("未配置 [bilibili] 凭据，跳过 B 站开放平台接入")
		return nil, nil
	}
	if !complete {
		return nil, errors.New("[bilibili] 已启用但凭据不完整：access_key / access_key_secret / id_code / app_id 都必须配置")
	}

	bilibili.SetLogger(log)

	client, err := bilibili.New(bilibili.Config{
		Host:              b.Host,
		AccessKey:         b.AccessKey,
		AccessKeySecret:   b.AccessKeySecret,
		IDCode:            b.IDCode,
		AppID:             b.AppID,
		HeartbeatInterval: b.HeartbeatInterval,
	}, dispatchBilibili)
	if err != nil {
		return nil, err
	}

	log.Sugar().Info("B 站开放平台接入已启用")
	return client, nil
}

// dispatchBilibili 把一条 B 站事件信封交给分发链。
//
// 与 WebSocket 接入路径等价，唯一区别是没有客户端可以写回 Action：事件依然要
// 走一遍 BeginDispatch / FinishDispatch 的顺序门控，否则分发期间产生的上传会
// 被永久卡在暂存区里（门控只等 FinishDispatch 放行）。
func dispatchBilibili(payload []byte) {
	dispatchCtx := upload.BeginDispatch(context.Background())
	_ = event.Dispatch(dispatchCtx, platformBilibili, payload)
	upload.FinishDispatch(dispatchCtx)
}
