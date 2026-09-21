// Package memory 提供会话的长期记忆（执行计划 E7）：追加写入的聊天记录 + 关键词召回。
//
// 存储形态是 JSON Lines（一行一条记录），不是 SQLite。这是对决策票 08 的修订：
// CGo 已被排除，纯 Go 的 SQLite 驱动要拖进十几个模块（含上百 MB 的 libc 源码），
// 而这里真正需要的只是「记住说过的话 + 按关键词找回来」。追加式文件零依赖、
// 可读可 grep，量级（一场直播几千条）也远没到需要索引结构的程度。
//
// 召回按关键词打分：中日韩文本没有分词，用字符二元组当词元（CJK 的常用近似），
// ASCII 按单词切。打分同时考虑命中的词元数、记录的权重（SC/礼物更高）与时间新旧。
package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry 是一条记忆：用户说了什么、当时怎么回的。
type Entry struct {
	Time      time.Time `json:"time"`
	ChannelID string    `json:"channel_id"`
	User      string    `json:"user"`
	Text      string    `json:"text"`
	Reply     string    `json:"reply"`
	// Weight 是重要度权重：普通弹幕 1，礼物/大航海更高，SC 最高。
	Weight int `json:"weight"`
}

// EntryWithID 是带 ID 的记忆条目：接口层按 ID 删除，ID 即文件行号。
//
// 内嵌 Entry，JSON 上是平铺的字段加一个 id。
type EntryWithID struct {
	ID int `json:"id"`
	Entry
}

// recordLine 是文件里一行的原始形态：要么是一条记忆，要么是一条删除标记。
type recordLine struct {
	Op string `json:"op"`
	ID int    `json:"id"`
	Entry
}

// opDelete 是删除标记的 op 取值。
const opDelete = "delete"

// record 是内存里的一条记忆：条目 + 它在文件中的行号。
//
// 行号即对外 ID：追加式文件里行号稳定（只追加、不重写），接口层靠它删除。
type record struct {
	id    int
	entry Entry
}

// Store 是记忆存储。
type Store struct {
	mu      sync.RWMutex
	path    string
	file    *os.File
	entries []record

	// nextID 是下一条记录的行号。
	nextID int

	// inverted 是词元到条目下标的倒排索引。
	inverted map[string][]int
}

// Open 打开（或新建）记忆文件，并把已有记录载入内存索引。
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("memory: 记忆文件路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建记忆目录: %w", err)
	}

	// ID 就是文件行号，从 1 起：空文件（或首次创建）时 nextID 必须是 1，
	// 否则内存里的 ID 会与重开后按行号算出的 ID 错位，墓碑就删错条目了
	store := &Store{path: path, inverted: make(map[string][]int), nextID: 1}
	if err := store.load(); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开记忆文件: %w", err)
	}
	store.file = file

	return store, nil
}

// load 读取已有记录并建索引。
func (s *Store) load() error {
	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取记忆文件: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	lineNo := 0
	records := make([]record, 0, 64)
	deleted := make(map[int]bool)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineNo++

		var raw recordLine
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			// 单行坏了不影响其余记录，跳过并继续
			continue
		}
		if raw.Op == opDelete {
			// 墓碑可能出现在被删记录之后，所以删除先收集、最后统一过滤
			deleted[raw.ID] = true
			continue
		}

		records = append(records, record{id: lineNo, entry: raw.Entry})
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	s.nextID = lineNo + 1
	for _, item := range records {
		if deleted[item.id] {
			continue
		}
		s.appendRecord(item)
	}

	return nil
}

// appendRecord 把一条记录加入内存，并把它的词元挂到倒排索引尾部。
//
// 调用方需持有写锁或处于初始化阶段。只追加，因此索引位置就是切片下标。
func (s *Store) appendRecord(item record) {
	position := len(s.entries)
	s.entries = append(s.entries, item)

	seen := make(map[string]bool)
	for _, token := range Tokenize(item.entry.Text + " " + item.entry.Reply) {
		if seen[token] {
			continue
		}
		seen[token] = true
		s.inverted[token] = append(s.inverted[token], position)
	}
}

// rebuildIndex 按当前条目重建倒排索引。
//
// 删除会改变下标，逐条挪索引比重建更容易出错（ponytail: O(n) 重建，删除是低频操作，
// 真到毫秒级瓶颈再谈增量维护）。
func (s *Store) rebuildIndex() {
	s.inverted = make(map[string][]int)

	for position, item := range s.entries {
		seen := make(map[string]bool)
		for _, token := range Tokenize(item.entry.Text + " " + item.entry.Reply) {
			if seen[token] {
				continue
			}
			seen[token] = true
			s.inverted[token] = append(s.inverted[token], position)
		}
	}
}

// Append 追加一条记忆，并落盘；返回它的 ID（文件行号）。
func (s *Store) Append(entry Entry) (int, error) {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	if entry.Weight <= 0 {
		entry.Weight = 1
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		return 0, fmt.Errorf("序列化记忆: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		return 0, fmt.Errorf("记忆文件已关闭")
	}

	// 先写盘再进内存：写失败时内存里不该出现一条磁盘上没有的记录
	if _, err := s.file.Write(append(payload, '\n')); err != nil {
		return 0, fmt.Errorf("写入记忆: %w", err)
	}

	id := s.nextID
	s.nextID++
	s.appendRecord(record{id: id, entry: entry})

	return id, nil
}

// Delete 删除一条记忆：追加一条墓碑记录，读取时据此过滤。
//
// 为什么不是重写文件：追加式存储的重写要写锁 + 临时文件原子替换，中途崩溃还要能回滚；
// 删除是低频操作，墓碑更简单，也留下了「什么时候删了哪条」的痕迹。代价是文件只增不减。
func (s *Store) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		return fmt.Errorf("记忆文件已关闭")
	}

	position := -1
	for i, item := range s.entries {
		if item.id == id {
			position = i
			break
		}
	}
	if position < 0 {
		return fmt.Errorf("记忆 %d 不存在", id)
	}

	payload, err := json.Marshal(recordLine{Op: opDelete, ID: id})
	if err != nil {
		return fmt.Errorf("序列化删除标记: %w", err)
	}
	if _, err := s.file.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("写入删除标记: %w", err)
	}

	s.entries = append(s.entries[:position], s.entries[position+1:]...)
	s.rebuildIndex()

	return nil
}

// Snapshot 返回最近的 limit 条记忆（新的在前，limit<=0 时取 20），带 ID 供接口层删除。
func (s *Store) Snapshot(limit int) []EntryWithID {
	if limit <= 0 {
		limit = defaultRecentLimit
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.entries) == 0 {
		return nil
	}

	start := len(s.entries) - limit
	if start < 0 {
		start = 0
	}

	out := make([]EntryWithID, 0, len(s.entries)-start)
	for i := len(s.entries) - 1; i >= start; i-- {
		out = append(out, EntryWithID{ID: s.entries[i].id, Entry: s.entries[i].entry})
	}

	return out
}

// Recall 按关键词召回相关记忆，最多返回 limit 条（limit<=0 时取 5）。
//
// 打分 = 命中的词元数 × 权重系数 ÷ 时间衰减；同分按时间倒序。
func (s *Store) Recall(query string, limit int) []EntryWithID {
	if limit <= 0 {
		limit = defaultRecallLimit
	}

	tokens := Tokenize(query)
	if len(tokens) == 0 {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	scores := make(map[int]float64)
	for _, token := range tokens {
		for _, position := range s.inverted[token] {
			scores[position]++
		}
	}
	if len(scores) == 0 {
		return nil
	}

	type scored struct {
		position int
		score    float64
	}
	ranked := make([]scored, 0, len(scores))
	for position, hits := range scores {
		entry := s.entries[position].entry
		ranked = append(ranked, scored{position: position, score: hits * weightFactor(entry.Weight) * recencyFactor(entry.Time)})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return s.entries[ranked[i].position].entry.Time.After(s.entries[ranked[j].position].entry.Time)
	})

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	out := make([]EntryWithID, 0, len(ranked))
	for _, item := range ranked {
		record := s.entries[item.position]
		out = append(out, EntryWithID{ID: record.id, Entry: record.entry})
	}

	return out
}

// Len 返回已记住的条目数。
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.entries)
}

// Close 关闭底层文件。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil

	return err
}

// defaultRecentLimit 是不带检索词时的返回条数。
const defaultRecentLimit = 20

const (
	// defaultRecallLimit 是未指定条数时召回几条。
	defaultRecallLimit = 5
	// recencyHalfLife 是时间衰减的半衰期。
	recencyHalfLife = 24 * time.Hour
)

// weightFactor 把重要度折算成打分系数。
func weightFactor(weight int) float64 {
	if weight <= 1 {
		return 1
	}

	return 1 + float64(weight-1)*0.5
}

// recencyFactor 越新越大：每过一个半衰期衰减一半。
func recencyFactor(at time.Time) float64 {
	age := time.Since(at)
	if age <= 0 {
		return 1
	}

	return 1 / (1 + age.Hours()/recencyHalfLife.Hours())
}

// Tokenize 把文本切成检索词元：ASCII 按单词，其余（中日韩）按字符二元组。
//
// 之所以用二元组：中文没有空格，不引入分词器就无法切词，而二元组在
// 「找回说过同一句话的片段」这个用途上足够准；单字也保留，短查询才有得匹配。
// 标点与空白作为切分点，不产生跨标点的词元。
func Tokenize(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}

	var (
		tokens []string
		word   []rune
		cjk    []rune
	)

	// ASCII 单词整体作为一个词元
	flushWord := func() {
		if len(word) > 0 {
			tokens = append(tokens, string(word))
			word = word[:0]
		}
	}
	// 一段中日韩文字：长度 ≥2 时只取相邻二元组，单字段才取单字。
	//
	// 不索引单字是有意的：中文单字里「的」「了」这类高频字会带来大量假命中
	// （查询「完全没有出现过的词」曾经因为一个「的」字命中了无关记录）。
	flushCJK := func() {
		if len(cjk) == 1 {
			tokens = append(tokens, string(cjk[0]))
		}
		for i := 0; i+1 < len(cjk); i++ {
			tokens = append(tokens, string(cjk[i:i+2]))
		}
		cjk = cjk[:0]
	}

	for _, r := range text {
		switch {
		case isWordRune(r):
			flushCJK()
			word = append(word, r)
		case isSeparator(r):
			flushWord()
			flushCJK()
		default:
			flushWord()
			cjk = append(cjk, r)
		}
	}
	flushWord()
	flushCJK()

	return tokens
}

// isWordRune 判断是否 ASCII 单词字符（字母与数字）。
func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// isSeparator 判断是否标点或空白：作为切分点，本身不进词元。
func isSeparator(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F: // CJK 标点
		return true
	case r >= 0xFF00 && r <= 0xFF0F: // 全角标点
		return true
	case r >= 0xFF1A && r <= 0xFF20:
		return true
	case r >= 0xFF3B && r <= 0xFF40:
		return true
	case r >= 0xFF5B && r <= 0xFF65:
		return true
	case r == ' ' || r == '\t' || r == '\n' || r == '\r':
		return true
	case r >= 0x21 && r <= 0x2F, r >= 0x3A && r <= 0x40: // ASCII 标点
		return true
	case r >= 0x5B && r <= 0x60, r >= 0x7B && r <= 0x7E:
		return true
	default:
		return false
	}
}
