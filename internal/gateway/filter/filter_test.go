package filter

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeWordsFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "words.txt")
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入词库文件: %v", err)
	}
	return path
}

func TestDeduplicator(t *testing.T) {
	d := NewDeduplicator(60 * time.Second)
	if d.Seen("qq:group_1:100") {
		t.Fatal("首次出现不应判重")
	}
	if !d.Seen("qq:group_1:100") {
		t.Fatal("窗口内重复应判重")
	}
	if d.Seen("qq:group_1:101") {
		t.Fatal("不同键不应判重")
	}
}

func TestDeduplicatorExpires(t *testing.T) {
	d := NewDeduplicator(100 * time.Millisecond)
	d.Seen("key")
	if !d.Seen("key") {
		t.Fatal("窗口内重复应判重")
	}
	time.Sleep(300 * time.Millisecond)
	if d.Seen("key") {
		t.Fatal("窗口过期后应放行")
	}
}

func TestDeduplicatorDisabled(t *testing.T) {
	d := NewDeduplicator(0)
	for i := 0; i < 3; i++ {
		if d.Seen("key") {
			t.Fatal("ttl<=0 时应完全禁用去重")
		}
	}
}

func TestFilterSensitiveWords(t *testing.T) {
	path := writeWordsFile(t, "", "   ", "# 注释行", "赌博", "法轮功")
	f, err := New(path, 0)
	if err != nil {
		t.Fatalf("创建过滤器: %v", err)
	}
	if !f.Sensitive("推荐一个赌博网站") {
		t.Fatal("应命中「赌博」")
	}
	if f.Sensitive("今天天气不错") {
		t.Fatal("不应误命中")
	}
}

func TestFilterNoWordsFile(t *testing.T) {
	f, err := New("", 0)
	if err != nil {
		t.Fatalf("空词库文件应直接可用: %v", err)
	}
	if f.Sensitive("任何文本") {
		t.Fatal("未配置词库时不应过滤")
	}
	if f.Duplicate("any") {
		t.Fatal("去重关闭时不应判重")
	}
}

func TestFilterMissingWordsFile(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "missing.txt"), 0); err == nil {
		t.Fatal("词库文件缺失应返回错误")
	}
}
