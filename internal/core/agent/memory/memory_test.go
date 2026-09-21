package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"空串", "", nil},
		{"英文按单词", "Hello World", []string{"hello", "world"}},
		{"中文只取二元组（不索引单字）", "你好", []string{"你好"}},
		{"标点切分", "你好，世界", []string{"你好", "世界"}},
		{"中英混排", "送火箭 gift", []string{"送火", "火箭", "gift"}},
		{"数字并入单词", "abc123", []string{"abc123"}},
		{"单字段才取单字", "猫", []string{"猫"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Tokenize(tc.text)
			if len(got) != len(tc.want) {
				t.Fatalf("Tokenize(%q) = %v, want %v", tc.text, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("Tokenize(%q) = %v, want %v", tc.text, got, tc.want)
				}
			}
		})
	}
}

func TestAppendAndRecall(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "memory.jsonl"))
	if err != nil {
		t.Fatalf("打开记忆: %v", err)
	}
	defer store.Close()

	entries := []Entry{
		{ChannelID: "room_1", User: "甲", Text: "主播今天玩什么游戏", Reply: "今天玩星露谷"},
		{ChannelID: "room_1", User: "乙", Text: "你最喜欢吃什么", Reply: "我喜欢吃拉面"},
		{ChannelID: "room_2", User: "丙", Text: "游戏区的主播真多", Reply: "是呀"},
	}
	for _, entry := range entries {
		if _, err := store.Append(entry); err != nil {
			t.Fatalf("写入: %v", err)
		}
	}

	if store.Len() != 3 {
		t.Fatalf("条目数 = %d, want 3", store.Len())
	}

	hits := store.Recall("游戏", 5)
	if len(hits) == 0 {
		t.Fatal("应该召回提到游戏的两条")
	}
	for _, hit := range hits {
		if hit.Text != "主播今天玩什么游戏" && hit.Text != "游戏区的主播真多" {
			t.Fatalf("召回了不相关的记录: %s", hit.Text)
		}
	}

	if hits := store.Recall("完全没有出现过的词", 5); len(hits) != 0 {
		t.Fatalf("无关查询不该有结果，得到 %d 条", len(hits))
	}
}

// 权重高的记录排前面：SC/礼物的内容更值得被想起来。
func TestRecallPrefersHeavierWeight(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "memory.jsonl"))
	if err != nil {
		t.Fatalf("打开记忆: %v", err)
	}
	defer store.Close()

	_, _ = store.Append(Entry{Text: "提到了拉面", Reply: "普通弹幕", Weight: 1})
	_, _ = store.Append(Entry{Text: "提到了拉面", Reply: "醒目留言", Weight: 5})

	hits := store.Recall("拉面", 5)
	if len(hits) != 2 {
		t.Fatalf("召回 %d 条, want 2", len(hits))
	}
	if hits[0].Reply != "醒目留言" {
		t.Fatalf("权重高的应排前面，实际第一条是 %q", hits[0].Reply)
	}
}

// 重开之后历史还在——这是「跨场次记忆」的基本要求。
func TestStoreReloadsFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.jsonl")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("打开记忆: %v", err)
	}
	if _, err := first.Append(Entry{Text: "记住我说过的话", Reply: "好的"}); err != nil {
		t.Fatalf("写入: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("关闭: %v", err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("重新打开: %v", err)
	}
	defer second.Close()

	if second.Len() != 1 {
		t.Fatalf("重新载入后条目数 = %d, want 1", second.Len())
	}
	if hits := second.Recall("记住", 3); len(hits) != 1 || hits[0].Reply != "好的" {
		t.Fatalf("重新载入后召回失败: %#v", hits)
	}
}

// 坏行不能带崩整个记忆：跳过它，其余记录照常可用。
func TestStoreSkipsBrokenLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.jsonl")
	content := `{"time":"2026-09-16T10:00:00Z","text":"好行","reply":"ok","weight":1}
这不是 JSON
{"time":"2026-09-16T10:01:00Z","text":"另一条好行","reply":"ok","weight":1}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试文件: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("打开记忆: %v", err)
	}
	defer store.Close()

	if store.Len() != 2 {
		t.Fatalf("条目数 = %d, want 2（坏行应被跳过）", store.Len())
	}
}

func TestRecallLimit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "memory.jsonl"))
	if err != nil {
		t.Fatalf("打开记忆: %v", err)
	}
	defer store.Close()

	for i := 0; i < 10; i++ {
		_, _ = store.Append(Entry{Text: "重复的话题", Reply: "回复", Time: time.Now().Add(time.Duration(i) * time.Minute)})
	}

	if hits := store.Recall("话题", 3); len(hits) != 3 {
		t.Fatalf("召回 %d 条, want 3", len(hits))
	}
	if hits := store.Recall("话题", 0); len(hits) != defaultRecallLimit {
		t.Fatalf("默认召回 %d 条, want %d", len(hits), defaultRecallLimit)
	}
}

// 删除写墓碑、读取时过滤：重开文件后结果必须一致（这是「文件只增不重写」的代价所在）。
func TestDeleteWritesTombstoneAndSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.jsonl")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("打开记忆: %v", err)
	}

	first, err := store.Append(Entry{Text: "第一条", Reply: "好的"})
	if err != nil {
		t.Fatalf("写入第一条: %v", err)
	}
	second, err := store.Append(Entry{Text: "第二条", Reply: "嗯"})
	if err != nil {
		t.Fatalf("写入第二条: %v", err)
	}
	if first == second {
		t.Fatalf("两条记录的 ID 相同: %d", first)
	}

	if err := store.Delete(second); err != nil {
		t.Fatalf("删除: %v", err)
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("删除后条数 = %d, want 1", got)
	}
	if hits := store.Recall("第二条", 5); len(hits) != 0 {
		t.Fatalf("被删除的记录仍能被召回: %#v", hits)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("关闭: %v", err)
	}

	// 墓碑必须落盘：重开后仍然是删掉的状态，且存活记录的 ID 不变
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读文件: %v", err)
	}
	if !strings.Contains(string(raw), `"op":"delete"`) {
		t.Fatalf("文件里没有墓碑记录:\n%s", raw)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("重新打开: %v", err)
	}
	defer reopened.Close()

	if got := reopened.Len(); got != 1 {
		t.Fatalf("重开后条数 = %d, want 1", got)
	}
	snapshot := reopened.Snapshot(10)
	if len(snapshot) != 1 || snapshot[0].ID != first {
		t.Fatalf("重开后快照 = %#v, want 仅 ID=%d 的那条", snapshot, first)
	}

	// 删除不存在的 ID 要报错，而不是静默成功
	if err := reopened.Delete(9999); err == nil {
		t.Fatal("删除不存在的 ID 应该报错")
	}
}
