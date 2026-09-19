# 上传管线三层改造 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在记忆服务上传的最前端增加「去重 + 敏感词过滤（前缀树）」层，并为最终上传增加「缓存背压」层与「远程写锁」层；上传请求只有在事件分发给出 Action 之后才允许获取写锁。

**Architecture:** 上传管线从前往后共三层：`internal/filter`（新包，rune 前缀树匹配敏感词库 + TTL 去重器）挂在 `upload` 入口最前；`internal/upload` 内把 `writeCh` 改为按配置容量创建的有界缓存，入队改为阻塞式背压（可配超时）；分发期间上传请求先暂存到 `DispatchScope`，server 在给出 Action（写回事件源客户端）后调用 `FinishDispatch` 放行入队，发送 worker 弹出请求后获取 `writeMu` 写锁再写远程。分发上下文（`context.Context`）从 `wsHandler` 贯穿 handler 链，作为暂存 scope 的载体，保证多连接并发下门控严格按事件归属。

**Tech Stack:** Go（go.mod 现有版本），spf13/viper 配置，coder/websocket，标准 `testing` 包。

## Global Constraints

- 注释与日志全部使用中文；文件名全小写无下划线；导出标识符 `PascalCase`；JSON tag 使用 `snake_case`。
- 错误用 `fmt.Errorf("描述: %w", err)` 包装；网络/解码错误记录日志，不中断处理链。
- 敏感词库文件为 UTF-8 纯文本，每行一个词，`#` 开头为注释行；空行忽略。
- 行为保持：所有事件仍会上传（零 Action 事件在分发完成时视为已给出 Action），`distillery` 路径不受影响。
- 配置默认值：`queue_size=256`、`queue_wait_timeout=0`（无限等待）、`dedup_ttl=600s`、`sensitive_words_file` 为空（不启用过滤）。
- 验证命令：`go build ./cmd/onebot-gateway/`、`go vet ./...`、`go test ./...`。
- 提交仅暂存本任务涉及的文件（在 `onebot-gateway/` 目录使用 git）。

---

### Task 1: 敏感词前缀树

**Files:**
- Create: `internal/filter/trie.go`
- Test: `internal/filter/trie_test.go`

**Interfaces:**
- Produces: `type Trie struct{}`；`func NewTrie() *Trie`；`func (t *Trie) Insert(word string)`；`func (t *Trie) Contains(text string) bool`（文本任意位置命中任意词库词即返回 true）

- [ ] **Step 1: 写失败测试**

```go
package filter

import "testing"

func TestTrieInsertAndContains(t *testing.T) {
	trie := NewTrie()
	trie.Insert("赌博")
	trie.Insert("法轮功")

	if !trie.Contains("他沉迷赌博无法自拔") {
		t.Fatal("应命中词库词「赌博」")
	}
	if !trie.Contains("法轮功是邪教") {
		t.Fatal("应命中词库词「法轮功」")
	}
	if trie.Contains("今天天气不错") {
		t.Fatal("不应误命中")
	}
	if trie.Contains("赌") {
		t.Fatal("前缀不应误命中：词库没有单字「赌」")
	}
}

func TestTrieSubstringMatch(t *testing.T) {
	trie := NewTrie()
	trie.Insert("天气")
	// 命中位置不在文本开头
	if !trie.Contains("今天天气不错") {
		t.Fatal("应从任意起始位置匹配「天气」")
	}
}

func TestTrieMultiByteAndEmptyWord(t *testing.T) {
	trie := NewTrie()
	trie.Insert("") // 空词不应 panic
	trie.Insert("人民币")
	if !trie.Contains("我想用人民币") {
		t.Fatal("应命中多字节词「人民币」")
	}
	if trie.Contains("人民") {
		t.Fatal("不应误命中「人民」")
	}
}

func TestTrieEmptyTrie(t *testing.T) {
	trie := NewTrie()
	if trie.Contains("任何文本") {
		t.Fatal("空词库不应命中")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd onebot-gateway && go test ./internal/filter/ -run Trie -v`
Expected: FAIL，`undefined: NewTrie`

- [ ] **Step 3: 实现 trie.go**

```go
package filter

// trieNode 前缀树节点，children 以 rune 为键，支持中文等多字节字符。
type trieNode struct {
	children map[rune]*trieNode
	terminal bool // 是否为某个敏感词的结尾
}

// Trie 基于 rune 的前缀树，用于敏感词匹配。
type Trie struct {
	root *trieNode
}

// NewTrie 创建空前缀树。
func NewTrie() *Trie {
	return &Trie{root: &trieNode{children: make(map[rune]*trieNode)}}
}

// Insert 插入一个敏感词，空词直接忽略。
func (t *Trie) Insert(word string) {
	node := t.root
	for _, r := range word {
		child, ok := node.children[r]
		if !ok {
			child = &trieNode{children: make(map[rune]*trieNode)}
			node.children[r] = child
		}
		node = child
	}
	node.terminal = true
}

// Contains 判断文本是否包含词库中的任意敏感词。
// 从文本的每个起始位置做一次前缀匹配，任一位置命中即返回 true。
// 词库规模较小时该实现足够；如需更高吞吐可后续升级为 AC 自动机。
func (t *Trie) Contains(text string) bool {
	runes := []rune(text)
	for i := range runes {
		node := t.root
		for j := i; j < len(runes); j++ {
			child, ok := node.children[runes[j]]
			if !ok {
				break
			}
			node = child
			if node.terminal {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd onebot-gateway && go test ./internal/filter/ -run Trie -v`
Expected: PASS（4 个用例）

- [ ] **Step 5: 提交**

```bash
cd onebot-gateway && git add internal/filter/trie.go internal/filter/trie_test.go && git commit -m "feat: 敏感词前缀树匹配"
```

---

### Task 2: 去重器与上传过滤器

**Files:**
- Create: `internal/filter/filter.go`
- Test: `internal/filter/filter_test.go`

**Interfaces:**
- Consumes: Task 1 的 `Trie`
- Produces: `func SetLogger(*zap.Logger)`；`type Deduplicator`；`func NewDeduplicator(ttl time.Duration) *Deduplicator`；`func (d *Deduplicator) Seen(key string) bool`；`type Filter`；`func New(wordsFile string, dedupTTL time.Duration) (*Filter, error)`；`func (f *Filter) Duplicate(key string) bool`；`func (f *Filter) Sensitive(text string) bool`

- [ ] **Step 1: 写失败测试**

```go
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
	d := NewDeduplicator(30 * time.Millisecond)
	d.Seen("key")
	if !d.Seen("key") {
		t.Fatal("窗口内重复应判重")
	}
	time.Sleep(50 * time.Millisecond)
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd onebot-gateway && go test ./internal/filter/ -run 'Dedup|Filter' -v`
Expected: FAIL，`undefined: NewDeduplicator`

- [ ] **Step 3: 实现 filter.go**

```go
package filter

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var log = zap.NewNop()

// SetLogger 注入日志器。
func SetLogger(l *zap.Logger) {
	if l != nil {
		log = l
	}
}

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
	trie *Trie
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
	log.Sugar().Infof("敏感词库已加载: 文件=%s 词数=%d", wordsFile, len(words))
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
		log.Sugar().Debugf("重复消息已丢弃: key=%s", key)
		return true
	}
	return false
}

// Sensitive 报告文本是否命中敏感词库。
func (f *Filter) Sensitive(text string) bool {
	return f.trie.Contains(text)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd onebot-gateway && go test ./internal/filter/ -v`
Expected: PASS（Task 1 与 Task 2 全部用例）

- [ ] **Step 5: 提交**

```bash
cd onebot-gateway && git add internal/filter/filter.go internal/filter/filter_test.go && git commit -m "feat: 上传前置去重与敏感词过滤"
```

---

### Task 3: 上传配置项扩展

**Files:**
- Modify: `internal/config/config.go:9-14`（`Memory` 结构体）
- Modify: `config.toml.example`（`[memory]` 段）

**Interfaces:**
- Produces: `config.Config.Memory` 新增字段 `QueueSize int`、`QueueWaitTimeout time.Duration`、`SensitiveWordsFile string`、`DedupTTL time.Duration`（mapstructure tag 见下）

- [ ] **Step 1: 扩展 Memory 配置结构**

```go
	Memory struct {
		Target             string        `mapstructure:"target"`
		CallbackPlatform   string        `mapstructure:"callback_platform"`
		QueueSize          int           `mapstructure:"queue_size"`           // 上传缓存容量，默认 256
		QueueWaitTimeout   time.Duration `mapstructure:"queue_wait_timeout"`   // 缓存满时等待上限，0=无限等待
		SensitiveWordsFile string        `mapstructure:"sensitive_words_file"` // 敏感词库文件路径，空=不启用
		DedupTTL           time.Duration `mapstructure:"dedup_ttl"`            // 去重窗口，0=不启用
	} `mapstructure:"memory"`
```

同时在 `config.go` 的 import 中加入 `"time"`。

- [ ] **Step 2: 更新 config.toml.example 的 [memory] 段**

```toml
[memory]
target = "ws://127.0.0.1:12393/proxy-ws"
# 记忆服务下行 Action 的目标客户端平台
callback_platform = "qq"
# 上传缓存容量（默认 256，0 也按 256 处理）
queue_size = 256
# 缓存满时的等待上限，0 = 无限等待（背压）；例如 "10s"
queue_wait_timeout = "0s"
# 敏感词库文件路径（UTF-8，每行一个词，# 开头为注释）；为空则不启用过滤
sensitive_words_file = ""
# 消息去重窗口，例如 "600s"；0 = 不启用
dedup_ttl = "600s"
```

- [ ] **Step 3: 编译验证**

Run: `cd onebot-gateway && go build ./... && go vet ./...`
Expected: 编译与静态检查通过

- [ ] **Step 4: 提交**

```bash
cd onebot-gateway && git add internal/config/config.go config.toml.example && git commit -m "feat: 上传管线配置项（缓存容量/等待超时/敏感词库/去重窗口）"
```

---

### Task 4: 上传管线重构（门控 + 缓存背压 + 远程写锁）

**Files:**
- Modify: `internal/upload/client.go`（`Init`、`upload`、`writeLoop` 及包级变量区）
- Create: `internal/upload/pipeline_test.go`
- Modify: `internal/app/app.go:25-26`（`upload.Init` 调用点，Task 5 前一并验证）

> 注（Task 5 执行期修正）：`client.go` 不再导入 `internal/server`——`handleCallbackAction` 的远程 Action 转发改为 `upload.SetActionForwarder` 注入回调（见 Task 5 Step 3b），解除 server↔upload 导入循环。

**Interfaces:**
- Consumes: Task 2 的 `filter.Filter`；Task 3 的配置字段
- Produces:
  - `type Options struct { QueueSize int; QueueWaitTimeout time.Duration; SensitiveWordsFile string; DedupTTL time.Duration }`
  - `func Init(target, callbackTargetPlatform string, opts Options) error`（替换原双参签名）
  - `func BeginDispatch(ctx context.Context) context.Context`
  - `func FinishDispatch(ctx context.Context)`
  - 包内 `func enqueue(payload []byte)`、`func scopeFrom(ctx context.Context) *DispatchScope`、`func upload(ctx context.Context, e platformEvent)`

- [ ] **Step 1: 写失败测试（门控、背压、直入队）**

```go
package upload

import (
	"context"
	"testing"
	"time"
)

// initTest 以最小配置初始化上传包，并注册测试清理。
func initTest(t *testing.T, opts Options) {
	t.Helper()
	if opts.QueueSize <= 0 {
		opts.QueueSize = 4
	}
	if err := Init("ws://127.0.0.1:1/test", "qq", opts); err != nil {
		t.Fatalf("初始化上传: %v", err)
	}
	t.Cleanup(Shutdown)
}

func TestDispatchScopeGatesUpload(t *testing.T) {
	initTest(t, Options{})
	ctx := BeginDispatch(context.Background())
	upload(ctx, platformEvent{Type: EventTypeMessage, PlatformName: "qq", ContentText: "你好"})

	if len(writeCh) != 0 {
		t.Fatalf("分发未完成时不应入队: 缓存长度=%d", len(writeCh))
	}
	FinishDispatch(ctx)
	if len(writeCh) != 1 {
		t.Fatalf("分发完成后应入队: 缓存长度=%d", len(writeCh))
	}
}

func TestUploadWithoutScopeEnqueuesDirectly(t *testing.T) {
	initTest(t, Options{})
	upload(context.Background(), platformEvent{Type: EventTypeMessage, PlatformName: "qq", ContentText: "你好"})
	if len(writeCh) != 1 {
		t.Fatalf("无分发上下文时应直接入队: 缓存长度=%d", len(writeCh))
	}
}

func TestEnqueueBackpressureTimeoutDrops(t *testing.T) {
	initTest(t, Options{QueueSize: 1, QueueWaitTimeout: 50 * time.Millisecond})
	enqueue([]byte("占位")) // 占满缓存

	start := time.Now()
	enqueue([]byte("第二条"))
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("背压未生效: 入队耗时=%v", elapsed)
	}
	if len(writeCh) != 1 {
		t.Fatalf("超时后应丢弃: 缓存长度=%d", len(writeCh))
	}
}

func TestEnqueueBackpressureBlocksUntilShutdown(t *testing.T) {
	initTest(t, Options{QueueSize: 1, QueueWaitTimeout: 0})
	enqueue([]byte("占位")) // 占满缓存

	released := make(chan struct{})
	go func() {
		enqueue([]byte("第二条"))
		close(released)
	}()

	select {
	case <-released:
		t.Fatal("缓存满时背压应阻塞入队")
	case <-time.After(100 * time.Millisecond):
	}
	Shutdown()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("关闭后应解除阻塞")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd onebot-gateway && go test ./internal/upload/ -run 'Scope|Backpressure' -v`
Expected: FAIL（`Init` 参数不匹配导致编译失败）

- [ ] **Step 3: 重构 client.go**

包级变量区改为（注意：`filter` 既是包级变量名又是导入包名，Go 禁止文件内同名遮蔽，因此导入使用别名 `wordfilter`）：

```go
// ---- 长连接管理 ----

var (
	wsConn           *websocket.Conn
	writeCh          chan []byte // 层1：有界缓存，Init 时按 QueueSize 创建
	writeMu          sync.Mutex  // 层2：远程写锁，同一时刻只允许一个在途上传
	done             = make(chan struct{})
	targetURL        string
	callbackPlatform string
	queueWaitTimeout time.Duration // 层1：缓存满时入队等待上限，0=无限等待
	filter           *wordfilter.Filter
)
```

`client.go` 的 import 增加（其余 import 不变）：

```go
	wordfilter "onebot-gateway/internal/filter"
```

// Options 上传管线配置。
type Options struct {
	QueueSize          int           // 缓存容量，<=0 按 256 处理
	QueueWaitTimeout   time.Duration // 缓存满时等待上限，0=无限等待
	SensitiveWordsFile string        // 敏感词库文件路径，空=不启用
	DedupTTL           time.Duration // 去重窗口，0=不启用
}

// DispatchScope 一次事件分发期间暂存的上传请求。
// 请求在 FinishDispatch 之前不允许进入缓存，保证「先给出 Action 再上传」。
type DispatchScope struct {
	staged [][]byte // 已序列化的待上传负载
}

type scopeKey struct{}
```

`Init` 改为（注意：重建 `done` 以支持测试多次初始化；生产环境只调用一次，行为不变）：

```go
// Init 建立到记忆服务的 WebSocket 长连接，启动后台读写协程。
// 连接断开后自动重连。opts 为上传管线配置。
// 目标地址为空时仅记录错误并跳过初始化（缓存仍创建，避免 nil 通道阻塞入队）。
func Init(target string, callbackTargetPlatform string, opts Options) error {
	size := opts.QueueSize
	if size <= 0 {
		size = 256
	}
	writeCh = make(chan []byte, size)
	done = make(chan struct{})

	if target == "" {
		log.Sugar().Error("上传目标地址为空，跳过初始化")
		return nil
	}
	targetURL = target
	callbackPlatform = callbackTargetPlatform
	queueWaitTimeout = opts.QueueWaitTimeout

	f, err := wordfilter.New(opts.SensitiveWordsFile, opts.DedupTTL)
	if err != nil {
		return err
	}
	filter = f

	go connectLoop()
	return nil
}
```

新增门控与入队逻辑（放在 `upload` 函数之前）：

```go
// BeginDispatch 创建本次事件分发的上传暂存区，并挂到返回的 ctx 上。
// 分发期间 handler 内的 upload 调用将请求暂存，等待 FinishDispatch 放行。
func BeginDispatch(ctx context.Context) context.Context {
	return context.WithValue(ctx, scopeKey{}, &DispatchScope{})
}

// FinishDispatch 放行本次分发暂存的上传请求。
// 调用方必须保证此时 Action 已经给出（已写回事件源客户端，或分发完成）。
func FinishDispatch(ctx context.Context) {
	scope, ok := ctx.Value(scopeKey{}).(*DispatchScope)
	if !ok || scope == nil {
		return
	}
	for _, payload := range scope.staged {
		enqueue(payload)
	}
	scope.staged = nil
}

// scopeFrom 从 ctx 取本次分发的暂存区；没有则返回 nil。
func scopeFrom(ctx context.Context) *DispatchScope {
	scope, _ := ctx.Value(scopeKey{}).(*DispatchScope)
	return scope
}

// enqueue 将负载写入有界缓存。
// 缓存满时按 queueWaitTimeout 等待（0=无限等待），实现背压；超时则丢弃并记录错误。
func enqueue(payload []byte) {
	if queueWaitTimeout <= 0 {
		select {
		case writeCh <- payload:
		case <-done:
			log.Sugar().Warn("上传缓存已满且网关关闭，丢弃事件")
		}
		return
	}
	timer := time.NewTimer(queueWaitTimeout)
	defer timer.Stop()
	select {
	case writeCh <- payload:
	case <-timer.C:
		log.Sugar().Error("上传缓存已满且等待超时，丢弃事件")
	case <-done:
		log.Sugar().Warn("上传缓存已满且网关关闭，丢弃事件")
	}
}
```

`upload` 函数改为（过滤前置 + 暂存或直入队）：

```go
func upload(ctx context.Context, e platformEvent) {
	// 层0：最前 —— 去重 + 敏感词过滤
	if filter != nil {
		key := e.PlatformName + ":" + e.ChannelID + ":" + e.MessageID
		if e.MessageID != "" && filter.Duplicate(key) {
			return
		}
		if filter.Sensitive(e.ContentText) {
			log.Sugar().Warnf("消息命中敏感词已丢弃: key=%s", key)
			return
		}
	}

	payload, err := json.Marshal(e)
	if err != nil {
		log.Sugar().Errorf("序列化平台事件失败: %v", err)
		return
	}

	// 分发期间：暂存，等待 Action 给出后放行；无分发上下文：直接入队
	if scope := scopeFrom(ctx); scope != nil {
		scope.staged = append(scope.staged, payload)
		return
	}
	enqueue(payload)
}
```

`writeLoop` 改为在写远程前获取写锁：

```go
func writeLoop(conn *websocket.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-done:
			return
		case payload := <-writeCh:
			// 层2：获取远程写锁，保证同一时刻只有一个在途上传
			writeMu.Lock()
			err := conn.Write(context.Background(), websocket.MessageText, payload)
			writeMu.Unlock()
			if err != nil {
				log.Sugar().Errorf("上传 WebSocket 写入失败: %v", err)
				return
			}
		}
	}
}
```

公共入口（`Upload`、`UploadBilibili*`、`UploadNotice`）签名不变，但把 ctx 传给 `upload(ctx, e)`（替换原先 `upload(e)` 调用，忽略参数 `_ context.Context` 改为具名 `ctx context.Context`）。`Shutdown`、`connectLoop`、`readLoop`、`handleCallbackAction` 及全部 `build*PlatformEvent` 构造函数不变。

- [ ] **Step 4: 更新 app.go 调用点**

```go
	upload.SetLogger(log)
	if err := upload.Init(cfg.Memory.Target, cfg.Memory.CallbackPlatform, upload.Options{
		QueueSize:          cfg.Memory.QueueSize,
		QueueWaitTimeout:   cfg.Memory.QueueWaitTimeout,
		SensitiveWordsFile: cfg.Memory.SensitiveWordsFile,
		DedupTTL:           cfg.Memory.DedupTTL,
	}); err != nil {
		return nil, err
	}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd onebot-gateway && go test ./internal/upload/ -v`
Expected: PASS（Task 4 四个用例；`go test ./...` 亦应全部通过——公共上传入口签名未变，register.go 无需改动）

- [ ] **Step 6: 提交**

```bash
cd onebot-gateway && git add internal/upload/client.go internal/upload/pipeline_test.go internal/app/app.go && git commit -m "feat: 上传管线加入 Action 门控、缓存背压与远程写锁"
```

---

### Task 5: 分发上下文贯穿 handler 链

**Files:**
- Modify: `internal/event/onebot/handlers.go:18-30`（`ActionList`/`Add`/`Run`/`DispatchWithHandlers`/`newActionList`）
- Modify: `internal/event/bilibililive/handlers.go:19-66`（同结构）
- Modify: `internal/event/onebot/dispatch.go`（`Dispatch` 及全部 `dispatchXxxEvent`/`decodeAndDispatch`）
- Modify: `internal/event/bilibililive/dispatch.go`（`Dispatch`/`decodeAndDispatch`）
- Modify: `internal/event/dispatch.go:18-30`（顶层 `Dispatch`）
- Modify: `internal/app/register.go`（全部 handler 签名）
- Modify: `internal/server/server.go:72-95`（`wsHandler` 接线）
- Modify: `internal/event/bilibililive/dispatch_test.go`（调用点）

**Interfaces:**
- Consumes: Task 4 的 `upload.BeginDispatch` / `upload.FinishDispatch`
- Produces: `event.Dispatch(ctx context.Context, platform string, raw []byte)`；`ActionList[T].Add(handlers ...func(context.Context, T) action.Action)`；`ActionList[T].Run(ctx context.Context, event T) action.Action`；`func SetActionForwarder(fn func(platform string, act action.Action) error)`

- [ ] **Step 1: 更新两个 handlers.go 的泛型列表签名**

`internal/event/onebot/handlers.go` 与 `internal/event/bilibililive/handlers.go` 相同改动：

```go
import (
	"context"
	"fmt"
	...
)

type ActionList[T any] struct {
	handlers []func(context.Context, T) action.Action
}

func newActionList[T any](eventType string) ActionList[T] {
	var list ActionList[T]
	list.Add(func(ctx context.Context, event T) action.Action {
		log.Sugar().Debugf("分发 %s 事件: %s", eventType, fmt.Sprint(event))
		return action.Action{}
	})
	return list
}

func (l *ActionList[T]) Add(handlers ...func(context.Context, T) action.Action) {
	l.handlers = append(l.handlers, handlers...)
}

func (l *ActionList[T]) Run(ctx context.Context, event T) action.Action {
	var result action.Action
	if l == nil {
		return result
	}
	for _, fn := range l.handlers {
		next := fn(ctx, event)
		if !isZeroAction(next) {
			result = next
		}
	}
	return result
}

func DispatchWithHandlers[T any](ctx context.Context, event T, handlers *ActionList[T]) action.Action {
	return handlers.Run(ctx, event)
}
```

- [ ] **Step 2: 更新 onebot/dispatch.go 与 bilibililive/dispatch.go**

`onebot/dispatch.go`：`Dispatch` 改为 `func Dispatch(ctx context.Context, raw []byte) action.Action`；`dispatchMetaEvent`/`dispatchMessageEvent`/`dispatchNoticeEvent`/`dispatchNotifyEvent`/`dispatchRequestEvent` 全部加 `ctx context.Context` 首参并逐级透传；`decodeAndDispatch` 改为：

```go
func decodeAndDispatch[T any](
	ctx context.Context,
	raw []byte,
	event *T,
	eventType string,
	handlers *ActionList[T],
) action.Action {
	if err := json.Unmarshal(raw, event); err != nil {
		return decodeFailed(eventType, err)
	}
	return DispatchWithHandlers(ctx, *event, handlers)
}
```

调用形如 `return decodeAndDispatch(ctx, raw, &event, "message group", &MessageGroupActions)`。

`bilibililive/dispatch.go`：`Dispatch` 改为 `func Dispatch(ctx context.Context, raw []byte) action.Action`；`decodeAndDispatch` 同样加 ctx 首参，调用形如 `return decodeAndDispatch(ctx, raw, &packet, "B站弹幕", &LiveOpenPlatformDMActions)`。

`internal/event/dispatch.go` 顶层：

```go
import "context"

func Dispatch(ctx context.Context, platform string, raw []byte) action.Action {
	switch platform {
	case PlatformQQ:
		return onebot.Dispatch(ctx, raw)
	case PlatformBilibili:
		return bilibililive.Dispatch(ctx, raw)
	default:
		log.Sugar().Debugf("未知平台 platform=%s", platform)
		return action.Action{}
	}
}
```

- [ ] **Step 3: 更新 register.go 全部 handler**

规则：每个 handler 首参加 `ctx context.Context`，内部 `upload.*` 调用把 `context.Background()` 换成 `ctx`（共 10 处），其余逻辑与 `distillery.Post` 调用不变。示例（QQ 群消息与通知注册）：

```go
	onebot.MessageGroupActions.Add(func(ctx context.Context, e onebot.MessageGroupEvent) action.Action {
		upload.Upload(ctx, platformQQ, e)
		return action.Action{}
	})
```

```go
func registerNotice[T any](actions *onebot.ActionList[T], platform string, record func(T) noticeRecord) {
	actions.Add(func(ctx context.Context, e T) action.Action {
		notice := record(e)
		upload.UploadNotice(ctx, platform, notice.userID, notice.text)
		return action.Action{}
	})
}
```

`registerBilibili` 内 7 处 handler 同规则：`upload.UploadBilibiliDM(ctx, ...)`、`upload.UploadBilibiliGift(ctx, ...)`、`upload.UploadBilibiliSuperChat(ctx, ...)`、`upload.UploadBilibiliGuard(ctx, ...)`、3 处 `upload.UploadBilibiliNotice(ctx, ...)`。`import` 增加 `"context"`，`context.Background()` 不再使用。

- [ ] **Step 3b: 解除 server↔upload 循环依赖（方案 B：依赖注入）**

`internal/server/server.go` 的 `wsHandler` 需要导入 `internal/upload`（调用 `BeginDispatch`/`FinishDispatch`），而 `internal/upload/client.go` 的 `readLoop` 又调用 `server.SendAction` 转发远程 Action（Task 4 明确保留），形成 `server → upload → server` 导入循环，`go build ./...` 直接失败。采用依赖注入解除：upload 不再导入 server，改为由 app 注入转发回调。

`internal/upload/client.go`：

```go
// 删除 import "onebot-gateway/internal/server"
```

包级变量区新增（中文注释）：

```go
	// forwardAction 远程 Action 转发回调，由 app 注入（server.SendAction），
	// 避免 upload 包导入 server 包造成循环依赖。
	forwardAction func(platform string, act action.Action) error
```

新增导出函数（放在 SetLogger 附近）：

```go
// SetActionForwarder 注入远程 Action 转发回调（server.SendAction），解除 server↔upload 循环依赖。
func SetActionForwarder(fn func(platform string, act action.Action) error) {
	forwardAction = fn
}
```

`handleCallbackAction` 末尾的 `server.SendAction(callbackPlatform, act)` 改为：

```go
	if forwardAction == nil {
		log.Sugar().Error("未注入 Action 转发回调，忽略远程 Action")
		return
	}
	if err := forwardAction(callbackPlatform, act); err != nil {
		log.Sugar().Errorf("转发远程 Action 失败: %v", err)
		return
	}
```

`internal/app/app.go`：在 `upload.SetLogger(log)` 之后、`upload.Init(...)` 之前插入 `upload.SetActionForwarder(server.SendAction)`（server 包已在 app.go 的 import 中）。`internal/server/server.go` 的 `upload` 导入因此合法。

- [ ] **Step 4: 更新 wsHandler 接线（给出 Action 后放行）**

`internal/server/server.go` 消息循环改为：

```go
	ctx := context.WithValue(r.Context(), platformKey, platform)
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			log.Sugar().Errorf("WebSocket 读取已结束: %v", err)
			return
		}
		log.Sugar().Debugf("收到 [%s] 消息: %d 字节", platform, len(msg))

		// 本次分发的上传暂存区；先给出 Action，再放行上传请求获取远程写锁
		dispatchCtx := upload.BeginDispatch(ctx)
		act := event.Dispatch(dispatchCtx, platform, msg)

		// 给出 Action：非零时写回事件源客户端；零 Action 视为分发完成
		if act.Action != "" {
			resp, err := json.Marshal(act)
			if err != nil {
				log.Sugar().Warnf("序列化响应失败: %v", err)
			} else if err := client.write(ctx, resp); err != nil {
				log.Sugar().Errorf("WebSocket 写入失败: %v", err)
				upload.FinishDispatch(dispatchCtx)
				return
			}
		}
		upload.FinishDispatch(dispatchCtx)
	}
```

`server.go` 的 `import` 增加 `"onebot-gateway/internal/upload"`。

- [ ] **Step 5: 更新 bilibililive/dispatch_test.go**

`Dispatch([]byte(tt.raw))` 改为 `Dispatch(context.Background(), []byte(tt.raw))`；所有 `Add(func(e XxxEvent) action.Action { ... })` 改为 `Add(func(ctx context.Context, e XxxEvent) action.Action { ... })`（首参 `_ context.Context` 亦可）；`import` 增加 `"context"`。

- [ ] **Step 6: 全量编译与测试**

Run: `cd onebot-gateway && go build ./... && go vet ./... && go test ./...`
Expected: 全部通过（Task 1-4 用例 + `TestSendActionForwardsStandardActionToConfiguredPlatformClient` + `TestDispatchRoutesSupportedEvents` + register_test 4 个用例）

- [ ] **Step 7: 提交**

```bash
cd onebot-gateway && git add internal/event internal/app/register.go internal/server/server.go && git commit -m "refactor: 分发上下文贯穿 handler 链，Action 给出后放行上传"
```

---

### Task 6: 文档更新

**Files:**
- Modify: `docs/UPLOAD_API.md`（第 2 节「本地队列」行与新增小节）
- Modify: `AGENTS.md`（「事件上传」小节）

- [ ] **Step 1: 更新 UPLOAD_API.md 第 2 节**

表格中「本地队列 | 容量 256 的内存缓冲通道」行改为「上传缓存 | 容量由 `[memory].queue_size` 配置（默认 256）的有界通道，满时阻塞入队（背压），等待上限由 `[memory].queue_wait_timeout` 配置（0=无限），超时丢弃并记录错误日志」。并在表格后补充一段「上传管线」说明：事件上传前先经去重（按 `platform_name:channel_id:message_id`，窗口 `[memory].dedup_ttl`）与敏感词过滤（`[memory].sensitive_words_file` 词库，前缀树匹配 `content_text`）；分发产生 Action 并写回事件源客户端后，本次事件的上传请求才允许获取远程写锁，同一时刻只有一个在途上传。

- [ ] **Step 2: 更新 AGENTS.md「事件上传」小节**

将「容量 256」与 fire-and-forget 描述改为：`upload.Init(target, callbackPlatform, Options)` 在 `app.Initialize()` 调用一次；上传管线为 去重+敏感词过滤（`internal/filter`）→ 有界缓存背压（`queue_size`/`queue_wait_timeout`）→ Action 顺序门控（`BeginDispatch`/`FinishDispatch`）→ 远程写锁（`writeMu`）。

- [ ] **Step 3: 提交**

```bash
cd onebot-gateway && git add docs/UPLOAD_API.md AGENTS.md && git commit -m "docs: 更新上传管线说明（过滤/背压/锁）"
```

---

### Task 7: 全量验证与收尾

- [ ] **Step 1: 全量检查**

Run: `cd onebot-gateway && go build ./cmd/onebot-gateway/ && go vet ./... && go test ./... && gofmt -l internal/ cmd/`
Expected: 编译通过、vet 无告警、全部测试 PASS、gofmt 无输出

- [ ] **Step 2: 自检规范**

- 中文注释与日志 ✓（filter 包、upload 包、register.go、server.go 新增代码）
- 文件名小写无下划线 ✓（`trie.go`/`filter.go`/`pipeline_test.go`）
- 无遗留 `context.Background()` 于 register.go ✓（grep 确认 `onebot-gateway/internal/app/register.go` 无 `context.Background`）
- `git status --short` 仅剩本任务文件，无意外改动

- [ ] **Step 3: 提交（如仍有未提交变更）**

```bash
cd onebot-gateway && git add -A internal/ config.toml.example docs/ AGENTS.md && git commit -m "chore: 上传管线三层改造收尾"
```

---

## Self-Review

**Spec 覆盖：**
- 最前一层「去重 + 敏感词过滤（前缀树）」→ Task 1（Trie）、Task 2（Deduplicator/Filter）、Task 4 Step 3（upload 入口接线）
- 缓存背压层 → Task 4（有界缓存 + 阻塞式入队 + 可配超时），测试 `TestEnqueueBackpressureTimeoutDrops`/`TestEnqueueBackpressureBlocksUntilShutdown`
- 远程写锁层 → Task 4（`writeMu`），门控语义 → `BeginDispatch`/`FinishDispatch` + Task 5 wsHandler 接线（先写回 Action 再放行；零 Action 事件分发完成即放行）
- 配置 → Task 3；文档 → Task 6；验证 → Task 7

**类型一致性：** `Options`、`BeginDispatch`/`FinishDispatch`（ctx 载体）、`enqueue`、`upload(ctx, e)`、`filter.Duplicate/Sensitive` 在 Task 4/5 间签名一致；`event.Dispatch(ctx, platform, raw)` 与 `ActionList.Run(ctx, event)` 在 Task 5 内逐级透传一致。

**已知取舍（执行时记录，不阻塞）：**
- 锁的直接并发互斥不做单测：`writeLoop` 是唯一消费者且每次写都持 `writeMu`，互斥由结构保证；门控与背压为新增逻辑，已有覆盖。
- 敏感词匹配为朴素 Trie 逐位置扫描（O(n·L)），词库规模小，足够；AC 自动机列为后续增强。
- 去重键依赖 `message_id` 非空（QQ 群消息与 B 站弹幕均有）；通知类事件无 `message_id`，跳过去重。
