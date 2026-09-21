package app

import (
	"github.com/Agentropism/vtuber-agent-go/internal/backend/web"
	"github.com/Agentropism/vtuber-agent-go/internal/core/config"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// provideFrontend 装配前端接入。
//
// 显式打开（[frontend].enabled）或配了模型目录时启用；返回 nil 表示不启用，
// 此时浏览器侧没有页面可开，播报只会写日志。
func provideFrontend(cfg *config.Config, character config.Character) (*web.Frontend, error) {
	f := cfg.Frontend
	if !f.Enabled && f.ModelsDir == "" {
		logger.Info("未配置 [frontend]，跳过前端接入")
		return nil, nil
	}

	instance, err := web.New(web.Config{
		Character: web.Character{
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
