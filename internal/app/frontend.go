package app

import (
	"github.com/Agentropism/vtuber-agent-go/internal/agent/frontend"
	"github.com/Agentropism/vtuber-agent-go/internal/config"

	"go.uber.org/zap"
)

// provideFrontend 装配前端接入。
//
// 显式打开（[frontend].enabled）或配了模型目录时启用；返回 nil 表示不启用，
// 此时浏览器侧没有页面可开，播报只会写日志。
func provideFrontend(cfg *config.Config, character config.Character, log *zap.Logger) (*frontend.Frontend, error) {
	f := cfg.Frontend
	if !f.Enabled && f.ModelsDir == "" {
		log.Sugar().Info("未配置 [frontend]，跳过前端接入")
		return nil, nil
	}

	frontend.SetLogger(log)

	instance, err := frontend.New(frontend.Config{
		Character: frontend.Character{
			Name:   character.Name,
			Avatar: character.Avatar,
		},
		ModelName:  character.Live2DModel,
		ModelsDir:  f.ModelsDir,
		ModelDict:  f.ModelDict,
		ModelScale: f.ModelScale,
	})
	if err != nil {
		return nil, err
	}

	return instance, nil
}
