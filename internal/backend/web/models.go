package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/emotion"
)

// modelEntry 是模型清单里的一项，字段对齐原项目根的 model_dict.json。
type modelEntry struct {
	Name       string            `json:"name"`
	URL        string            `json:"url"`
	Scale      float64           `json:"kScale"`
	XOffset    float64           `json:"kXOffset"`
	InitialX   float64           `json:"initialXshift"`
	InitialY   float64           `json:"initialYshift"`
	IdleMotion string            `json:"idleMotionGroupName"`
	EmotionMap orderedEmotionMap `json:"emotionMap"`
}

// orderedEmotionMap 是保留 JSON 键顺序的表情映射。
//
// 用切片而不是 map：原实现依赖 dict 的插入序决定「同名标签取哪一个」，
// Go 的 map 迭代顺序随机，直接用会改变行为。
type orderedEmotionMap []emotion.Entry

// UnmarshalJSON 按出现顺序解析表情映射。
func (m *orderedEmotionMap) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))

	if _, err := decoder.Token(); err != nil { // 左花括号
		return err
	}

	var entries []emotion.Entry
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("表情标签不是字符串: %v", keyToken)
		}

		var index int
		if err := decoder.Decode(&index); err != nil {
			return fmt.Errorf("表达式下标 %s 不是整数: %w", name, err)
		}
		entries = append(entries, emotion.Entry{Name: name, Index: index})
	}

	*m = entries
	return nil
}

// modelInfo 是下发给前端的模型信息。
type modelInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Scale 是相对「适配视口」的倍率：1.0 表示模型正好占满视口高度。
	Scale       float64  `json:"scale"`
	XShift      float64  `json:"x_shift"` // 水平微调（像素）
	YShift      float64  `json:"y_shift"` // 垂直微调（像素）
	IdleMotion  string   `json:"idle_motion"`
	Expressions []string `json:"expressions"` // 表达式名，按 model3.json 的顺序
}

// loadCatalog 选出要加载的模型，并返回它的表情词表。
//
// 两条来源：优先读 model_dict.json（原项目就有的资产，含 scale/偏移/emotionMap），
// 读不到就扫描 modelsDir 下的 <模型名>/runtime/*.model3.json 兜底，此时表情词表用内置默认值。
func loadCatalog(dictPath, modelsDir, want string) (modelEntry, *emotion.Map, error) {
	if entries, err := readModelDict(dictPath); err != nil {
		logger.Warnf("读取模型清单 %s 失败，改为扫描模型目录: %v", dictPath, err)
	} else if len(entries) > 0 {
		entry := pickModel(entries, want)
		return entry, emotion.NewMap(entry.EmotionMap), nil
	}

	entries, err := scanModels(modelsDir)
	if err != nil {
		return modelEntry{}, nil, err
	}
	if len(entries) == 0 {
		return modelEntry{}, nil, fmt.Errorf("在 %s 下没有找到任何 *.model3.json", modelsDir)
	}

	entry := pickModel(entries, want)
	return entry, emotion.Default(), nil
}

// readModelDict 读取 model_dict.json；路径为空或文件不存在时返回空。
func readModelDict(dictPath string) ([]modelEntry, error) {
	if strings.TrimSpace(dictPath) == "" {
		return nil, nil
	}

	raw, err := os.ReadFile(dictPath)
	if err != nil {
		return nil, err
	}

	var entries []modelEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}

	for i := range entries {
		entries[i].URL = normalizeModelURL(entries[i].URL)
	}

	return entries, nil
}

// normalizeModelURL 把清单里的模型地址换成当前的挂载前缀。
//
// model_dict.json 是外部文件（原项目根目录那份），里面的 URL 自带 /live2d-models/ 前缀；
// 直接照搬会让页面去请求一个本服务没有的路径（404），模型加载失败。
func normalizeModelURL(raw string) string {
	relative := strings.TrimPrefix(raw, "/")
	if index := strings.Index(relative, "/"); index >= 0 {
		relative = relative[index+1:] // 去掉外来的挂载前缀，保留 <模型名>/runtime/<文件>
	}
	if relative == "" {
		return raw
	}

	return path.Join(modelsPrefix, relative)
}

// pickModel 选出目标模型；want 为空或找不到时取第一个。
func pickModel(entries []modelEntry, want string) modelEntry {
	if want != "" {
		for _, entry := range entries {
			if entry.Name == want {
				return entry
			}
		}
		logger.Warnf("模型清单里没有 %q，改用 %q", want, entries[0].Name)
	}

	return entries[0]
}

// scanModels 扫描模型目录，构造最简清单。
func scanModels(modelsDir string) ([]modelEntry, error) {
	if strings.TrimSpace(modelsDir) == "" {
		return nil, fmt.Errorf("未配置模型目录")
	}

	matches, err := filepath.Glob(filepath.Join(modelsDir, "*", "runtime", "*.model3.json"))
	if err != nil {
		return nil, err
	}

	entries := make([]modelEntry, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		name := filepath.Base(filepath.Dir(filepath.Dir(match)))
		if seen[name] {
			continue
		}
		seen[name] = true

		entries = append(entries, modelEntry{
			Name:       name,
			URL:        path.Join(modelsPrefix, name, "runtime", filepath.Base(match)),
			Scale:      0.5,
			IdleMotion: "Idle",
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	return entries, nil
}

// loadExpressionNames 从 model3.json 里读出表达式名，供前端按名字切换表情。
func loadExpressionNames(modelsDir, url string) []string {
	relative := strings.TrimPrefix(url, modelsPrefix+"/")
	raw, err := os.ReadFile(filepath.Join(modelsDir, filepath.FromSlash(relative)))
	if err != nil {
		logger.Debugf("读取模型文件失败，跳过表达式清单: %v", err)
		return nil
	}

	var doc struct {
		FileReferences struct {
			Expressions []struct {
				Name string `json:"Name"`
			} `json:"Expressions"`
		} `json:"FileReferences"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		logger.Debugf("解析模型文件失败，跳过表达式清单: %v", err)
		return nil
	}

	names := make([]string, 0, len(doc.FileReferences.Expressions))
	for _, expression := range doc.FileReferences.Expressions {
		names = append(names, expression.Name)
	}

	return names
}
