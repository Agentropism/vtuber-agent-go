package emotion

import (
	"reflect"
	"testing"
)

func TestExtractNames(t *testing.T) {
	m := Default()

	cases := []struct {
		name string
		text string
		want []string
	}{
		{"单个标签", "[joy]你好", []string{"joy"}},
		{"标签在中间", "你好[smirk]呀", []string{"smirk"}},
		{"多个标签按出现顺序", "[sadness]唉[joy]好耶", []string{"sadness", "joy"}},
		{"大小写不敏感", "[JOY]你好", []string{"joy"}},
		{"允许重复", "[joy]一[joy]二", []string{"joy", "joy"}},
		{"词表外的标签不算", "[happy]你好", nil},
		{"括号不完整不算", "[joy 你好", nil},
		{"没有标签", "普通文本", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.ExtractNames(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExtractNames(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestExtractIndexes(t *testing.T) {
	m := Default()

	got := m.Extract("[joy]一[sadness]二")
	want := []int{3, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Extract = %v, want %v", got, want)
	}

	if m.Extract("没有标签") != nil {
		t.Fatal("没有标签时应返回 nil")
	}
}

func TestStrip(t *testing.T) {
	m := Default()

	cases := []struct {
		text string
		want string
	}{
		{"[joy]你好呀", "你好呀"},
		{"你好[smirk]呀", "你好呀"},
		{"[JOY]大小写不影响删除", "大小写不影响删除"},
		{"[joy][joy]重复标签都删掉", "重复标签都删掉"},
		{"[happy]词表外的标签保留", "[happy]词表外的标签保留"},
		{"没有标签", "没有标签"},
	}

	for _, tc := range cases {
		if got := m.Strip(tc.text); got != tc.want {
			t.Fatalf("Strip(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestIndex(t *testing.T) {
	m := Default()

	if got, ok := m.Index("joy"); !ok || got != 3 {
		t.Fatalf("Index(joy) = %d, %v", got, ok)
	}
	if got, ok := m.Index("JOY"); !ok || got != 3 {
		t.Fatalf("Index 应忽略大小写，得到 %d, %v", got, ok)
	}
	if _, ok := m.Index("happy"); ok {
		t.Fatal("词表外的标签不应命中")
	}
	if _, ok := m.Index(""); ok {
		t.Fatal("空标签不应命中")
	}
}

// 顺序是契约的一部分：同名标签重复声明时按词表顺序取第一个
// （原 Python 实现依赖 dict 的插入序，Go 的 map 保不住，所以用切片）。
func TestExtractRespectsOrder(t *testing.T) {
	m := NewMap([]Entry{{Name: "joy", Index: 1}, {Name: "joy", Index: 3}})

	if got := m.Extract("[joy]"); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("应按词表顺序取第一个，得到 %v", got)
	}
	if index, ok := m.Index("joy"); !ok || index != 1 {
		t.Fatalf("Index(joy) = %d, %v, want 1", index, ok)
	}
}

func TestNilMapIsSafe(t *testing.T) {
	var m *Map

	if m.Len() != 0 || m.Entries() != nil {
		t.Fatal("nil 映射的长度应为 0")
	}
	if m.Extract("[joy]") != nil || m.ExtractNames("[joy]") != nil {
		t.Fatal("nil 映射不应解析出标签")
	}
	if got := m.Strip("[joy]你好"); got != "[joy]你好" {
		t.Fatalf("nil 映射不应改动文本，得到 %q", got)
	}
	if _, ok := m.Index("joy"); ok {
		t.Fatal("nil 映射不应命中任何标签")
	}
}
