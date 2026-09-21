// Package emotion 定义表情标签契约：文本里用 [joy] 这样的方括号标签表达表情。
//
// 放在 shared 的原因：标签由对话侧（生成回复时剥离标签并带上表情）与前端侧
// （把标签换成 Live2D 表达式下标）共同使用，两侧必须对同一套词表达成一致。
//
// 词表本身来自模型：Live2D 模型的 model_dict.json 里 emotionMap 决定有哪些标签、
// 各自对应哪个表达式下标。本包只提供「有序映射 + 文本解析」，不绑定具体模型。
package emotion

import "strings"

// Entry 是一个表情标签与它对应的 Live2D 表达式下标。
type Entry struct {
	Name  string // 标签名，例如 "joy"
	Index int    // 表达式下标，对应模型 model3.json 的 FileReferences.Expressions
}

// Map 是表情标签的有序映射。
//
// 必须有序：文本里同时出现多个标签时，命中顺序会影响结果，而原 Python 实现
// 依赖 dict 的插入序，Go 的 map 无法保证顺序，因此这里用切片保存。
type Map struct {
	entries []Entry
}

// NewMap 用给定条目构造映射，保持传入顺序。
func NewMap(entries []Entry) *Map {
	return &Map{entries: append([]Entry(nil), entries...)}
}

// Entries 返回全部条目（副本）。
func (m *Map) Entries() []Entry {
	if m == nil {
		return nil
	}

	return append([]Entry(nil), m.entries...)
}

// Len 返回标签数量。
func (m *Map) Len() int {
	if m == nil {
		return 0
	}

	return len(m.entries)
}

// Index 按标签名查表达式下标，大小写不敏感。
func (m *Map) Index(name string) (int, bool) {
	if m == nil || name == "" {
		return 0, false
	}

	for _, entry := range m.entries {
		if strings.EqualFold(entry.Name, name) {
			return entry.Index, true
		}
	}

	return 0, false
}

// ExtractNames 返回文本里出现的表情标签名，按出现顺序，允许重复。
//
// 行为对齐原实现 extract_emotion：只认完整方括号标签（[joy]），逐字符扫描，
// 匹配时按词表顺序取第一个命中的标签。
func (m *Map) ExtractNames(text string) []string {
	if m == nil || text == "" {
		return nil
	}

	var names []string
	for i := 0; i < len(text); {
		if text[i] != '[' {
			i++
			continue
		}

		matched := false
		for _, entry := range m.entries {
			tag := "[" + entry.Name + "]"
			if hasPrefixFold(text[i:], tag) {
				names = append(names, entry.Name)
				i += len(tag)
				matched = true
				break
			}
		}
		if !matched {
			i++
		}
	}

	return names
}

// Extract 返回文本里出现的表达式下标，按出现顺序，允许重复。
func (m *Map) Extract(text string) []int {
	names := m.ExtractNames(text)
	if len(names) == 0 {
		return nil
	}

	indexes := make([]int, 0, len(names))
	for _, name := range names {
		if index, ok := m.Index(name); ok {
			indexes = append(indexes, index)
		}
	}

	return indexes
}

// Strip 去掉文本里的全部表情标签，其余内容原样保留（含原始大小写）。
func (m *Map) Strip(text string) string {
	if m == nil || text == "" {
		return text
	}

	for _, entry := range m.entries {
		tag := "[" + entry.Name + "]"
		for {
			index := indexFold(text, tag)
			if index < 0 {
				break
			}
			text = text[:index] + text[index+len(tag):]
		}
	}

	return text
}

// Default 是内置的兜底词表，取自 mao_pro 模型的 emotionMap。
//
// 模型清单（model_dict.json）读不到时用它，至少让常见标签可用。
func Default() *Map {
	return NewMap([]Entry{
		{Name: "neutral", Index: 0},
		{Name: "anger", Index: 2},
		{Name: "disgust", Index: 2},
		{Name: "fear", Index: 1},
		{Name: "joy", Index: 3},
		{Name: "smirk", Index: 3},
		{Name: "sadness", Index: 1},
		{Name: "surprise", Index: 3},
	})
}

// hasPrefixFold 判断 s 是否以 needle 开头，忽略大小写。
//
// needle 只含 ASCII，因此按字节比较是安全的：UTF-8 的续字节都 ≥ 0x80，
// 不可能与 ASCII 字符混淆。
func hasPrefixFold(s, needle string) bool {
	if len(s) < len(needle) {
		return false
	}

	return strings.EqualFold(s[:len(needle)], needle)
}

// indexFold 返回 needle 在 s 中首次出现的位置（忽略大小写），没有则返回 -1。
func indexFold(s, needle string) int {
	for i := 0; i+len(needle) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(needle)], needle) {
			return i
		}
	}

	return -1
}
