package filter

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/logger"
)

// Deduplicator 简单去重器：记录最近见过的键，窗口过期后自动遗忘。
// 内部加锁，可被多个分发协程并发使用。
type Deduplicator struct {
	mu   sync.Mutex
	ttl  time.Duration
	seen map[string]time.Time // 键 → 首次记录时间
}

// NewDeduplicator 创建去重器；ttl<=0 表示禁用去重。
func NewDeduplicator(ttl time.Duration) *Deduplicator {
	return &Deduplicator{ttl: ttl, seen: make(map[string]time.Time)}
}

// Seen 查询并记录：键在窗口内出现过返回 true；否则记录并返回 false。
func (d *Deduplicator) Seen(key string) bool {
	if d.ttl <= 0 || key == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if t, ok := d.seen[key]; ok && now.Sub(t) < d.ttl {
		return true
	}
	d.seen[key] = now
	// 惰性清理过期键，避免 map 无界增长
	if len(d.seen) > 4096 {
		for k, t := range d.seen {
			if now.Sub(t) >= d.ttl {
				delete(d.seen, k)
			}
		}
	}
	return false
}

// Filter 上传前置过滤器：去重 + 敏感词过滤。
type Filter struct {
	trie  *Trie
	dedup *Deduplicator
}

// New 创建过滤器；wordsFile 为空时不启用敏感词过滤，dedupTTL<=0 时不启用去重。
func New(wordsFile string, dedupTTL time.Duration) (*Filter, error) {
	f := &Filter{trie: NewTrie(), dedup: NewDeduplicator(dedupTTL)}
	if wordsFile == "" {
		return f, nil
	}
	words, err := loadWordsFile(wordsFile)
	if err != nil {
		return nil, fmt.Errorf("加载敏感词库 %s: %w", wordsFile, err)
	}
	for _, w := range words {
		f.trie.Insert(w)
	}
	logger.Infof("敏感词库已加载: 文件=%s 词数=%d", wordsFile, len(words))
	return f, nil
}

// loadWordsFile 逐行读取词库：去首尾空白，跳过空行与 # 注释行。
func loadWordsFile(path string) ([]string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	var words []string
	scanner := bufio.NewScanner(fh)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words = append(words, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取词库文件: %w", err)
	}
	return words, nil
}

// Duplicate 报告键是否在去重窗口内重复；键为空时直接放行。
func (f *Filter) Duplicate(key string) bool {
	if f.dedup.Seen(key) {
		logger.Debugf("重复消息已丢弃: key=%s", key)
		return true
	}
	return false
}

// Sensitive 报告文本是否命中敏感词库。
func (f *Filter) Sensitive(text string) bool {
	return f.trie.Contains(text)
}
