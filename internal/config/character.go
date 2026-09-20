package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Character 是一个角色定义。
//
// 对应原项目 characters/*.yaml 与 conf.yaml 的 character_config。单独成文件的原因：
// 角色是资产而不是部署配置——换角色不该动端口、凭据这类部署参数，而人设提示词、
// 头像、Live2D 模型名是一组必须一起换的东西。
type Character struct {
	Name         string `toml:"name"`          // 显示名
	Avatar       string `toml:"avatar"`        // 头像文件名，前端展示用
	Live2DModel  string `toml:"live2d_model"`  // Live2D 模型目录名，前端加载用
	SystemPrompt string `toml:"system_prompt"` // 人设系统提示词
}

// LoadCharacter 读取角色定义文件（TOML）。
//
// 路径为空时返回零值且不报错：此时调用方回退到 [agent].system_prompt 的行内提示词。
func LoadCharacter(path string) (Character, error) {
	var character Character
	if strings.TrimSpace(path) == "" {
		return character, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return character, fmt.Errorf("读取角色定义 %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &character); err != nil {
		return character, fmt.Errorf("解析角色定义 %s: %w", path, err)
	}
	return character, nil
}
