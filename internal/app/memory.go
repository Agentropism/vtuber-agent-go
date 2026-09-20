package app

import (
	"github.com/Agentropism/vtuber-agent-go/internal/agent/conversation"
	"github.com/Agentropism/vtuber-agent-go/internal/agent/memory"
	"github.com/Agentropism/vtuber-agent-go/internal/config"

	"github.com/Agentropism/vtuber-agent-go/internal/logger"
)

// memoryAdapter 把 memory.Store 适配成会话层要的记忆接口。
//
// 适配放在装配层：会话层定义自己要什么（接口归调用方所有），存储层只提供
// 自己的实现，两边都不需要认识对方的类型。
type memoryAdapter struct {
	store *memory.Store
}

func (m memoryAdapter) Recall(query string, limit int) []conversation.MemoryEntry {
	hits := m.store.Recall(query, limit)

	entries := make([]conversation.MemoryEntry, 0, len(hits))
	for _, hit := range hits {
		entries = append(entries, conversation.MemoryEntry{
			Time:      hit.Time,
			ChannelID: hit.ChannelID,
			User:      hit.User,
			Text:      hit.Text,
			Reply:     hit.Reply,
			Weight:    hit.Weight,
		})
	}

	return entries
}

func (m memoryAdapter) Remember(entry conversation.MemoryEntry) {
	if err := m.store.Append(memory.Entry{
		Time:      entry.Time,
		ChannelID: entry.ChannelID,
		User:      entry.User,
		Text:      entry.Text,
		Reply:     entry.Reply,
		Weight:    entry.Weight,
	}); err != nil {
		logger.Warnf("写入记忆失败: %v", err)
	}
}

// provideMemory 打开长期记忆；未配置路径时返回 nil。
func provideMemory(cfg *config.Config) (*memory.Store, error) {
	if cfg.Agent.MemoryFile == "" {
		logger.Info("未配置 [agent].memory_file，跳过长期记忆")
		return nil, nil
	}

	store, err := memory.Open(cfg.Agent.MemoryFile)
	if err != nil {
		return nil, err
	}
	logger.Infof("长期记忆已启用: %s（已有 %d 条记录）", cfg.Agent.MemoryFile, store.Len())

	return store, nil
}
