package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/archive"
	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/conversation"
	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
	"github.com/Agentropism/vtuber-agent-go/internal/core/gateway/upload"
	"github.com/Agentropism/vtuber-agent-go/internal/core/shared/event"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// archiveAdapter 把归档存储适配成会话层要的归档接口。
//
// 适配放在装配层：会话层定义自己要什么（接口归调用方所有），存储层只提供
// 自己的实现，两边都不需要认识对方的类型。
type archiveAdapter struct {
	store *archive.Store
}

func (a archiveAdapter) RecordDialogue(record conversation.ArchiveRecord) {
	err := a.store.Record(archive.Record{
		Kind:        archive.KindDialogue,
		Time:        record.Time,
		Platform:    record.Platform,
		ChannelID:   record.ChannelID,
		ChannelType: record.ChannelType,
		UserID:      record.UserID,
		UserName:    record.UserName,
		EventKind:   record.EventKind,
		Text:        record.Text,
		Reply:       record.Reply,
	})
	if err != nil {
		logger.Warnf("写入对话归档失败: %v", err)
	}
}

func (a archiveAdapter) RecentDialogues(channelID string, limit int) []conversation.ArchiveRecord {
	records := a.store.RecentDialogues(channelID, limit)

	out := make([]conversation.ArchiveRecord, 0, len(records))
	for _, record := range records {
		out = append(out, conversation.ArchiveRecord{
			Time:        record.Time,
			Platform:    record.Platform,
			ChannelID:   record.ChannelID,
			ChannelType: record.ChannelType,
			UserID:      record.UserID,
			UserName:    record.UserName,
			EventKind:   record.EventKind,
			Text:        record.Text,
			Reply:       record.Reply,
		})
	}

	return out
}

// archiveEnvelope 是归档包装器需要的事件信封字段子集。
type archiveEnvelope struct {
	Type         string     `json:"type"`
	Kind         event.Kind `json:"event_kind"`
	PlatformName string     `json:"platform_name"`
	ChannelID    string     `json:"channel_id"`
	ChannelType  string     `json:"channel_type"`
	UserID       string     `json:"user_id"`
	UserName     string     `json:"user_name"`
	SenderName   string     `json:"sender_name"`
	ContentText  string     `json:"content_text"`
	IsSelf       bool       `json:"is_self"`
	Timestamp    int64      `json:"timestamp"`
}

// archiveUploadHandler 包装上传终端：不产生回复的事件先归档，再交给会话层。
//
// 需要回复的事件（弹幕/群消息/礼物/SC/大航海）由会话层在轮次结束时连同回复归档，
// 这里必须跳过，否则同一条事件会被记两次。
func archiveUploadHandler(store *archive.Store, next upload.Handler) upload.Handler {
	return func(payload []byte) {
		if record, ok := eventArchiveRecord(payload); ok {
			if err := store.Record(record); err != nil {
				logger.Warnf("写入事件归档失败: %v", err)
			}
		}
		next(payload)
	}
}

// eventArchiveRecord 把「不产生回复」的事件转成归档记录。
//
// 机器人自身消息、需要回复的事件都返回 false；兼容尚未写入 event_kind 的
// 生产端时按 type=notice 判据回退（与会话层 ParseInbound 的取舍一致）。
func eventArchiveRecord(payload []byte) (archive.Record, bool) {
	var envelope archiveEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return archive.Record{}, false
	}
	if envelope.IsSelf {
		return archive.Record{}, false
	}

	kind := envelope.Kind
	if kind == "" {
		if envelope.Type != "notice" {
			return archive.Record{}, false
		}
		kind = event.KindNotice
	}
	if kind.NeedsReply() {
		return archive.Record{}, false
	}

	at := time.Now()
	if envelope.Timestamp > 0 {
		at = time.Unix(envelope.Timestamp, 0)
	}

	name := envelope.SenderName
	if name == "" {
		name = envelope.UserName
	}

	return archive.Record{
		Kind:        archive.KindEvent,
		Time:        at,
		Platform:    envelope.PlatformName,
		ChannelID:   envelope.ChannelID,
		ChannelType: envelope.ChannelType,
		UserID:      envelope.UserID,
		UserName:    name,
		EventKind:   kind,
		Text:        envelope.ContentText,
	}, true
}

// provideArchive 打开对话归档；未配置 [agent].archive_dir 时返回 nil。
func provideArchive(cfg *config.Config) (*archive.Store, error) {
	if strings.TrimSpace(cfg.Agent.ArchiveDir) == "" {
		logger.Info("未配置 [agent].archive_dir，跳过对话归档")
		return nil, nil
	}

	store, err := archive.Open(cfg.Agent.ArchiveDir)
	if err != nil {
		return nil, err
	}
	logger.Infof("对话归档已启用: %s", cfg.Agent.ArchiveDir)

	return store, nil
}
