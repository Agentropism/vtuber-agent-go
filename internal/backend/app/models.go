package app

import (
	"github.com/Agentropism/vtuber-agent-go/internal/backend/api"
	"github.com/Agentropism/vtuber-agent-go/internal/backend/web"
)

// modelStateFunc 把前端接入的模型状态转成接口层形态。
//
// web.modelInfo 是包内类型（字段导出），所以这里逐字段搬运而不是直接转型：
// 接口层不该认识 web 的内部类型。
func modelStateFunc(front *web.Frontend) func() (api.ModelState, error) {
	if front == nil {
		return nil
	}

	return func() (api.ModelState, error) {
		available, err := front.AvailableModels()
		if err != nil {
			return api.ModelState{}, err
		}

		models := make([]api.ModelInfo, 0, len(available))
		for _, item := range available {
			models = append(models, api.ModelInfo{Name: item.Name, URL: item.URL, Scale: item.Scale})
		}

		current := front.Model()

		return api.ModelState{
			Current: api.ModelInfo{
				Name:        current.Name,
				URL:         current.URL,
				Scale:       current.Scale,
				XShift:      current.XShift,
				YShift:      current.YShift,
				IdleMotion:  current.IdleMotion,
				Expressions: current.Expressions,
			},
			Models:   models,
			Emotions: front.EmotionLabels(),
		}, nil
	}
}

// modelSwitchFunc 装配模型热切换。
func modelSwitchFunc(front *web.Frontend) func(name string) error {
	if front == nil {
		return nil
	}

	return func(name string) error {
		_, err := front.SwitchModel(name)
		return err
	}
}

// emotionShowFunc 装配表情预览：标签换下标后下发给已连接的前端。
func emotionShowFunc(front *web.Frontend) func(label string) (int, error) {
	if front == nil {
		return nil
	}

	return front.PreviewEmotion
}
