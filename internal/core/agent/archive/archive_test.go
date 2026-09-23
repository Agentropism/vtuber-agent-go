package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/event"
)

// TestClassify 覆盖分类规则：保留渠道、前缀规则、自定义平台与兜底。
func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		channelID string
		platform  string
		want      string
	}{
		{"本地调试渠道精确匹配", ChannelLocal, "bilibili", CategoryLocal},
		{"QQ 群前缀", "group_10001", "qq", CategoryQQ},
		{"B 站直播间前缀", "room_12345", "bilibili", CategoryBilibili},
		{"自定义平台用平台名", "user_1", "qq2", "qq2"},
		{"无渠道无平台归入兜底", "", "", CategoryOther},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.channelID, tc.platform); got != tc.want {
				t.Fatalf("Classify(%q, %q) = %q, want %q", tc.channelID, tc.platform, got, tc.want)
			}
		})
	}
}

// TestRecordClassifiesIntoFiles 覆盖落盘分类：不同渠道写进各自分类与文件。
func TestRecordClassifiesIntoFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("打开归档: %v", err)
	}

	records := []Record{
		{Kind: KindDialogue, Platform: "bilibili", ChannelID: ChannelLocal, EventKind: event.KindDanmaku, Text: "本地调试", Reply: "收到"},
		{Kind: KindDialogue, Platform: "qq", ChannelID: "group_10001", EventKind: event.KindGroupMessage, Text: "群消息", Reply: "你好"},
		{Kind: KindEvent, Platform: "bilibili", ChannelID: "room_12345", EventKind: event.KindLike, Text: "[点赞] x1"},
		{Kind: KindEvent, Platform: "qq", ChannelID: "", EventKind: event.KindNotice, Text: "有人入群"},
	}
	for _, rec := range records {
		if err := store.Record(rec); err != nil {
			t.Fatalf("写入归档: %v", err)
		}
	}

	expectFiles := []string{
		filepath.Join(dir, "local", "room_local.jsonl"),
		filepath.Join(dir, "qq", "group_10001.jsonl"),
		filepath.Join(dir, "bilibili", "room_12345.jsonl"),
		filepath.Join(dir, "qq", "_notices.jsonl"),
	}
	for _, path := range expectFiles {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("归档文件缺失 %s: %v", path, err)
		}
	}
}

// TestReadNewestFirstAndPagination 覆盖读取顺序与分页（limit / before）。
func TestReadNewestFirstAndPagination(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("打开归档: %v", err)
	}

	base := time.Date(2026, 9, 21, 22, 0, 0, 0, time.Local)
	for i := 0; i < 5; i++ {
		err := store.Record(Record{
			Kind:      KindDialogue,
			Time:      base.Add(time.Duration(i) * time.Minute),
			Platform:  "qq",
			ChannelID: "group_1",
			EventKind: event.KindGroupMessage,
			Text:      "第 " + string(rune('1'+i)) + " 条",
			Reply:     "回复",
		})
		if err != nil {
			t.Fatalf("写入归档: %v", err)
		}
	}

	// 新的在前
	got, found, err := store.Read("qq", "group_1", 3, time.Time{})
	if err != nil || !found {
		t.Fatalf("读取归档: found=%v err=%v", found, err)
	}
	if len(got) != 3 {
		t.Fatalf("条数 = %d, want 3", len(got))
	}
	if got[0].Text != "第 5 条" || got[2].Text != "第 3 条" {
		t.Fatalf("顺序不符合预期: %q ... %q", got[0].Text, got[2].Text)
	}

	// before 只取该时间之前的记录
	before := base.Add(3 * time.Minute)
	got, _, err = store.Read("qq", "group_1", 100, before)
	if err != nil {
		t.Fatalf("按时间读取: %v", err)
	}
	if len(got) != 3 || got[0].Text != "第 3 条" {
		t.Fatalf("before 过滤不符合预期: %+v", got)
	}

	// 不存在的渠道：found=false，不是错误
	if _, found, err := store.Read("qq", "group_missing", 10, time.Time{}); err != nil || found {
		t.Fatalf("缺失渠道应返回 found=false，实际 found=%v err=%v", found, err)
	}
}

// TestRecordSanitizesNames 覆盖路径安全：渠道名里的分隔符与点号不能逃出归档目录。
func TestRecordSanitizesNames(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("打开归档: %v", err)
	}

	evil := "../../evil"
	if err := store.Record(Record{Kind: KindEvent, Platform: "bilibili", ChannelID: evil, EventKind: event.KindLike, Text: "x"}); err != nil {
		t.Fatalf("写入归档: %v", err)
	}

	// 归档目录之外不应出现任何文件
	parent := filepath.Dir(dir)
	if _, err := os.Stat(filepath.Join(parent, "evil.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("渠道名穿越了归档目录: %v", err)
	}

	// 同名渠道仍能按原渠道号读回
	records, found, err := store.Read("bilibili", evil, 10, time.Time{})
	if err != nil || !found || len(records) != 1 {
		t.Fatalf("清洗后的渠道应可读回: found=%v len=%d err=%v", found, len(records), err)
	}
}

// TestRecordConcurrent 覆盖并发写入：多协程写同一渠道，行数与内容必须完整。
func TestRecordConcurrent(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("打开归档: %v", err)
	}

	const writers, perWriter = 8, 25
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				_ = store.Record(Record{
					Kind:      KindDialogue,
					Platform:  "qq",
					ChannelID: "group_concurrent",
					EventKind: event.KindGroupMessage,
					Text:      "并发写入",
					Reply:     "ok",
				})
			}
		}(w)
	}
	wg.Wait()

	path := filepath.Join(dir, "qq", "group_concurrent.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取归档文件: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != writers*perWriter {
		t.Fatalf("行数 = %d, want %d", len(lines), writers*perWriter)
	}
	for _, line := range lines {
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("存在损坏行: %q: %v", line, err)
		}
	}
}

// TestRecentDialogues 覆盖恢复上下文用读取：只要对话、按时间正序、截断到 limit。
func TestRecentDialogues(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("打开归档: %v", err)
	}

	base := time.Date(2026, 9, 21, 22, 0, 0, 0, time.Local)
	for i := 0; i < 4; i++ {
		_ = store.Record(Record{
			Kind:      KindDialogue,
			Time:      base.Add(time.Duration(i) * time.Minute),
			Platform:  "qq",
			ChannelID: "group_1",
			EventKind: event.KindGroupMessage,
			Text:      "第 " + string(rune('1'+i)) + " 轮",
			Reply:     "回复",
		})
	}
	// 事件不该混进恢复上下文
	_ = store.Record(Record{
		Kind:      KindEvent,
		Time:      base.Add(10 * time.Minute),
		Platform:  "qq",
		ChannelID: "group_1",
		EventKind: event.KindLike,
		Text:      "[点赞] x1",
	})

	got := store.RecentDialogues("group_1", 2)
	if len(got) != 2 {
		t.Fatalf("条数 = %d, want 2", len(got))
	}
	if got[0].Text != "第 3 轮" || got[1].Text != "第 4 轮" {
		t.Fatalf("应为最近两轮且正序: %q, %q", got[0].Text, got[1].Text)
	}

	if got := store.RecentDialogues("group_missing", 2); len(got) != 0 {
		t.Fatalf("缺失渠道应为空: %+v", got)
	}
}

// TestChannelsSummary 覆盖 /api/chats 所需的摘要：计数、最后一条时间与文本。
func TestChannelsSummary(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("打开归档: %v", err)
	}

	base := time.Date(2026, 9, 21, 22, 0, 0, 0, time.Local)
	_ = store.Record(Record{Kind: KindDialogue, Time: base, Platform: "qq", ChannelID: "group_1", EventKind: event.KindGroupMessage, Text: "早", Reply: "早呀"})
	_ = store.Record(Record{Kind: KindDialogue, Time: base.Add(time.Minute), Platform: "qq", ChannelID: "group_1", EventKind: event.KindGroupMessage, Text: "在吗", Reply: "在的"})
	_ = store.Record(Record{Kind: KindEvent, Time: base, Platform: "bilibili", ChannelID: "room_1", EventKind: event.KindLike, Text: "[点赞] x1"})

	platforms, err := store.Channels()
	if err != nil {
		t.Fatalf("扫描归档: %v", err)
	}
	if len(platforms) != 2 {
		t.Fatalf("分类数 = %d, want 2: %+v", len(platforms), platforms)
	}
	if platforms[0].Platform != "bilibili" || platforms[1].Platform != "qq" {
		t.Fatalf("分类顺序不符合预期: %+v", platforms)
	}

	qq := platforms[1].Channels
	if len(qq) != 1 || qq[0].ChannelID != "group_1" || qq[0].Count != 2 {
		t.Fatalf("QQ 摘要不符合预期: %+v", qq)
	}
	if qq[0].LastText != "在吗" || !qq[0].LastTime.Equal(base.Add(time.Minute)) {
		t.Fatalf("最后一条摘要不符合预期: %+v", qq[0])
	}
}

// TestExport 覆盖两种导出格式与缺失渠道。
func TestExport(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("打开归档: %v", err)
	}

	at := time.Date(2026, 9, 21, 23, 0, 0, 0, time.Local)
	_ = store.Record(Record{Kind: KindDialogue, Time: at, Platform: "qq", ChannelID: "group_1", EventKind: event.KindGroupMessage, UserName: "小明", Text: "你好", Reply: "你好呀"})
	_ = store.Record(Record{Kind: KindEvent, Time: at, Platform: "qq", ChannelID: "group_1", EventKind: event.KindLike, UserName: "观众甲", Text: "[点赞] x3"})

	var jsonl bytes.Buffer
	found, err := store.Export("qq", "group_1", "jsonl", &jsonl)
	if err != nil || !found {
		t.Fatalf("导出 jsonl: found=%v err=%v", found, err)
	}
	if strings.Count(jsonl.String(), "\n") != 2 {
		t.Fatalf("jsonl 应为两行: %q", jsonl.String())
	}

	var markdown bytes.Buffer
	if _, err := store.Export("qq", "group_1", "md", &markdown); err != nil {
		t.Fatalf("导出 md: %v", err)
	}
	for _, want := range []string{"# 对话归档：qq/group_1", "小明", "用户：你好", "Mili：你好呀", "观众甲（like）"} {
		if !strings.Contains(markdown.String(), want) {
			t.Fatalf("md 缺少 %q:\n%s", want, markdown.String())
		}
	}

	// 整个分类导出：两个渠道拼接
	_ = store.Record(Record{Kind: KindEvent, Time: at, Platform: "qq", ChannelID: "group_2", EventKind: event.KindNotice, Text: "有人入群"})
	var whole bytes.Buffer
	found, err = store.Export("qq", "", "jsonl", &whole)
	if err != nil || !found {
		t.Fatalf("导出整个分类: found=%v err=%v", found, err)
	}
	if strings.Count(whole.String(), "\n") != 3 {
		t.Fatalf("整个分类应有 3 行: %q", whole.String())
	}

	if found, err := store.Export("qq", "group_missing", "jsonl", &bytes.Buffer{}); err != nil || found {
		t.Fatalf("缺失渠道: found=%v err=%v", found, err)
	}
	if _, err := store.Export("qq", "group_1", "pdf", &bytes.Buffer{}); err == nil {
		t.Fatal("不支持的格式应报错")
	}
}

// TestOpenRejectsEmptyDir 覆盖构造校验。
func TestOpenRejectsEmptyDir(t *testing.T) {
	if _, err := Open("  "); err == nil {
		t.Fatal("空目录应报错")
	}
}
