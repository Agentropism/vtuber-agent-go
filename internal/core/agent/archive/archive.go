// Package archive 提供对话归档：只增不删的 JSON Lines 记录，按平台/渠道分类存储。
//
// 归档与长期记忆（memory 包）是两件事：记忆是可删改的召回索引，归档是完整的
// 对话与事件流水，永久保留、可导出，不参与召回。因此这里没有删除接口，
// 也没有内存索引——写入是一次追加，读取是扫描文件。
//
// 存储布局（分类规则见 Classify）：
//
//	<dir>/local/room_local.jsonl      本地文本框调试
//	<dir>/qq/group_123456.jsonl       QQ
//	<dir>/bilibili/room_789.jsonl     B 站
//	<dir>/qq/_notices.jsonl           渠道号为空的通知事件
package archive

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/event"
)

// 记录类型：一行 JSON 的 kind 字段。
const (
	// KindDialogue 是一轮有回复的对话（弹幕/群消息/礼物/SC/大航海）。
	KindDialogue = "dialogue"
	// KindEvent 是不产生回复的事件（点赞/进房/开播下播/通知等）。
	KindEvent = "event"
)

// 分类目录与保留渠道的约定取值。
const (
	// ChannelLocal 是本地文本框调试的保留渠道号，精确匹配归入 local 分类。
	ChannelLocal = "room_local"
	// ChannelNotices 是渠道号为空的事件（QQ 群通知）的兜底归档名。
	ChannelNotices = "_notices"

	// CategoryLocal / CategoryQQ / CategoryBilibili 是归档的一级分类目录。
	CategoryLocal    = "local"
	CategoryQQ       = "qq"
	CategoryBilibili = "bilibili"
	// CategoryOther 是既无渠道前缀又无平台名时的兜底分类。
	CategoryOther = "other"
)

// DefaultReadLimit 是读取记录时的默认条数。
const DefaultReadLimit = 100

// ErrUnsupportedFormat 表示导出格式不支持。
var ErrUnsupportedFormat = errors.New("archive: 不支持的导出格式")

// Record 是归档里的一行记录。
//
// dialogue 带 reply，event 不带；未填充的可选字段省略，保持文件紧凑可读。
type Record struct {
	Kind        string     `json:"kind"`                   // dialogue / event
	Time        time.Time  `json:"time"`                   // 事件发生时间（通知类事件取落库时间）
	Platform    string     `json:"platform"`               // qq / bilibili / 自定义平台名
	ChannelID   string     `json:"channel_id"`             // group_123 / room_456 / room_local
	ChannelType string     `json:"channel_type,omitempty"` // group / live_room
	UserID      string     `json:"user_id,omitempty"`
	UserName    string     `json:"user_name,omitempty"`
	EventKind   event.Kind `json:"event_kind"` // 事件种类，取值见 shared/event
	Text        string     `json:"text"`
	Reply       string     `json:"reply,omitempty"` // 仅 dialogue
}

// ChannelInfo 是单个渠道的归档摘要。
type ChannelInfo struct {
	ChannelID string    `json:"channel_id"`
	Count     int       `json:"count"`
	LastTime  time.Time `json:"last_time,omitempty"`
	LastText  string    `json:"last_text,omitempty"`
}

// PlatformInfo 是一个分类下的全部渠道。
type PlatformInfo struct {
	Platform string        `json:"platform"`
	Channels []ChannelInfo `json:"channels"`
}

// Store 是归档存储：一个根目录，写时分类。
type Store struct {
	dir string

	// mu 串行化同进程内的写入与读取；记录频率低，全局一把锁足够。
	mu sync.Mutex
}

// Open 打开（或新建）归档目录。
func Open(dir string) (*Store, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("archive: 归档目录为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建归档目录: %w", err)
	}

	return &Store{dir: dir}, nil
}

// Dir 返回归档根目录。
func (s *Store) Dir() string { return s.dir }

// Classify 决定一条记录归到哪个分类目录。
//
// room_local 是本地调试的保留渠道；group_ / room_ 前缀分别归 QQ 与 B 站；
// 其余（自定义平台）直接用事件里的平台名，最后兜底 other。
func Classify(channelID, platform string) string {
	switch {
	case channelID == ChannelLocal:
		return CategoryLocal
	case strings.HasPrefix(channelID, event.ChannelPrefixGroup):
		return CategoryQQ
	case strings.HasPrefix(channelID, event.ChannelPrefixRoom):
		return CategoryBilibili
	case strings.TrimSpace(platform) != "":
		return platform
	default:
		return CategoryOther
	}
}

// Record 追加一条记录：先分类，再写入 <dir>/<分类>/<渠道>.jsonl。
//
// 每次打开-写入-关闭：记录频率低（一场直播几千条），换来的是不持有长驻句柄、
// 外部改文件后下次读取立即可见。同进程内由 Store.mu 串行化，不会写坏行。
func (s *Store) Record(rec Record) error {
	if rec.Time.IsZero() {
		rec.Time = time.Now()
	}
	if rec.Kind == "" {
		rec.Kind = KindDialogue
	}

	channel := rec.ChannelID
	if strings.TrimSpace(channel) == "" {
		channel = ChannelNotices
	}
	path := s.channelPath(Classify(rec.ChannelID, rec.Platform), channel)

	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("序列化归档记录: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建归档分类目录: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开归档文件: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("写入归档: %w", err)
	}

	return nil
}

// Channels 扫描归档目录，返回每个分类下的渠道摘要（计数、最后一条时间与文本）。
func (s *Store) Channels() ([]PlatformInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("读取归档目录: %w", err)
	}

	platforms := make([]PlatformInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		channels, err := s.scanPlatform(entry.Name())
		if err != nil {
			return nil, err
		}
		if len(channels) == 0 {
			continue
		}

		platforms = append(platforms, PlatformInfo{Platform: entry.Name(), Channels: channels})
	}

	sort.Slice(platforms, func(i, j int) bool { return platforms[i].Platform < platforms[j].Platform })

	return platforms, nil
}

// scanPlatform 扫描一个分类目录下的全部渠道文件。调用方需持有锁。
func (s *Store) scanPlatform(platform string) ([]ChannelInfo, error) {
	dir := filepath.Join(s.dir, platform)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取归档分类 %s: %w", platform, err)
	}

	channels := make([]ChannelInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		records, err := readRecords(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("读取归档 %s/%s: %w", platform, entry.Name(), err)
		}
		if len(records) == 0 {
			continue
		}

		last := records[len(records)-1]
		channels = append(channels, ChannelInfo{
			ChannelID: strings.TrimSuffix(entry.Name(), ".jsonl"),
			Count:     len(records),
			LastTime:  last.Time,
			LastText:  last.Text,
		})
	}

	sort.Slice(channels, func(i, j int) bool { return channels[i].ChannelID < channels[j].ChannelID })

	return channels, nil
}

// Read 返回某渠道的记录：新的在前，最多 limit 条；before 非零时只取该时间之前的记录。
//
// 第二个返回值表示该渠道的归档文件是否存在——不存在的渠道与空渠道对接口层
// 是两回事（404 与空列表）。
func (s *Store) Read(platform, channel string, limit int, before time.Time) ([]Record, bool, error) {
	if limit <= 0 {
		limit = DefaultReadLimit
	}

	path := s.channelPath(platform, channel)

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("查看归档文件: %w", err)
	}

	records, err := readRecords(path)
	if err != nil {
		return nil, true, fmt.Errorf("读取归档: %w", err)
	}

	matched := make([]Record, 0, len(records))
	for _, rec := range records {
		if !before.IsZero() && !rec.Time.Before(before) {
			continue
		}
		matched = append(matched, rec)
	}

	start := len(matched) - limit
	if start < 0 {
		start = 0
	}
	out := make([]Record, 0, len(matched)-start)
	for i := len(matched) - 1; i >= start; i-- {
		out = append(out, matched[i])
	}

	return out, true, nil
}

// RecentDialogues 返回某渠道最近 limit 轮对话，按时间正序（旧→新），供会话恢复上下文。
//
// 渠道的分类理论上可由渠道号推出，但自定义平台名可能落在任意分类目录，
// 稳妥起见扫描全部分类找同名文件，合并后按时间排序。
func (s *Store) RecentDialogues(channelID string, limit int) []Record {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" || limit <= 0 {
		return nil
	}
	channel := sanitizeName(channelID)

	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}

	dialogues := make([]Record, 0, limit)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		records, err := readRecords(filepath.Join(s.dir, entry.Name(), channel+".jsonl"))
		if err != nil {
			continue
		}
		for _, rec := range records {
			if rec.Kind == KindDialogue {
				dialogues = append(dialogues, rec)
			}
		}
	}
	if len(dialogues) == 0 {
		return nil
	}

	sort.SliceStable(dialogues, func(i, j int) bool { return dialogues[i].Time.Before(dialogues[j].Time) })
	if len(dialogues) > limit {
		dialogues = dialogues[len(dialogues)-limit:]
	}

	return dialogues
}

// Export 把某渠道（channel 为空时整个分类）的记录写进 w，返回是否存在内容。
//
// format 取 jsonl（默认）或 md；md 供人阅读，带每轮对话的缩进与事件单行。
func (s *Store) Export(platform, channel, format string, w io.Writer) (bool, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "jsonl"
	}
	if format != "jsonl" && format != "md" {
		return false, fmt.Errorf("%w: %q", ErrUnsupportedFormat, format)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	platformDir := filepath.Join(s.dir, sanitizeName(platform))
	if strings.TrimSpace(channel) != "" {
		records, err := readRecords(filepath.Join(platformDir, sanitizeName(channel)+".jsonl"))
		if err != nil {
			return false, fmt.Errorf("读取归档: %w", err)
		}
		if len(records) == 0 {
			return false, nil
		}
		if err := writeExport(w, records, format, platform, channel); err != nil {
			return false, err
		}
		return true, nil
	}

	entries, err := os.ReadDir(platformDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("读取归档分类: %w", err)
	}

	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		records, err := readRecords(filepath.Join(platformDir, entry.Name()))
		if err != nil {
			return false, fmt.Errorf("读取归档: %w", err)
		}
		if len(records) == 0 {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".jsonl")
		if err := writeExport(w, records, format, platform, name); err != nil {
			return false, err
		}
		found = true
	}

	return found, nil
}

// writeExport 写出一个渠道的内容；md 带标题，jsonl 逐行序列化。
func writeExport(w io.Writer, records []Record, format, platform, channel string) error {
	if format == "md" {
		if _, err := fmt.Fprintf(w, "# 对话归档：%s/%s\n\n", platform, channel); err != nil {
			return err
		}
		for _, rec := range records {
			if err := writeMarkdownLine(w, rec); err != nil {
				return err
			}
		}
		return nil
	}

	for _, rec := range records {
		payload, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("序列化归档记录: %w", err)
		}
		if _, err := w.Write(append(payload, '\n')); err != nil {
			return err
		}
	}

	return nil
}

// writeMarkdownLine 写出一条记录的 Markdown 形态。
func writeMarkdownLine(w io.Writer, rec Record) error {
	stamp := rec.Time.Format("2006-01-02 15:04:05")
	who := rec.UserName
	if who == "" {
		who = rec.UserID
	}

	if rec.Kind == KindEvent {
		_, err := fmt.Fprintf(w, "- %s %s（%s）：%s\n", stamp, who, rec.EventKind, rec.Text)
		return err
	}

	_, err := fmt.Fprintf(w, "- %s %s（%s）\n  - 用户：%s\n  - Mili：%s\n", stamp, who, rec.EventKind, rec.Text, rec.Reply)
	return err
}

// channelPath 拼出渠道的归档文件路径。
func (s *Store) channelPath(category, channel string) string {
	return filepath.Join(s.dir, sanitizeName(category), sanitizeName(channel)+".jsonl")
}

// sanitizeName 把路径片段里不安全的字符替换成下划线，杜绝目录穿越。
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)

	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}

	// 首尾的点会让 ".." 这类名字逃出目录，直接抹掉；抹空后归入兜底分类。
	out := strings.Trim(builder.String(), ".")
	if out == "" {
		return CategoryOther
	}

	return out
}

// readRecords 读取一个归档文件；文件不存在返回空。单行损坏只跳过该行。
func readRecords(path string) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	records := make([]Record, 0, 64)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return records, nil
}
