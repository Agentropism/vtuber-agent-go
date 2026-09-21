package conversation

import (
	"context"
	"strings"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// 主动发言的默认提示词与检查周期。
const (
	defaultIdlePrompt = "直播间安静了很久，请主动说一句轻松的话活跃气氛。只输出要说的话本身，不要解释。"
	// idleCheckInterval 是检查是否该主动发言的周期上限。
	idleCheckInterval = 30 * time.Second
)

// startIdleLoop 启动主动发言循环：渠道静默够久且期间没主动说过，就说一句。
//
// 发言走最低优先级（PriorityIdle），任何弹幕、礼物、SC 都能打断它；
// 主动说的话只进播报，不作为平台回复下发（直播间本来也没有发送接口）。
func (s *Sessions) startIdleLoop() {
	interval := s.cfg.IdleSpeakInterval
	if interval <= 0 {
		return
	}

	check := interval / 4
	if check <= 0 || check > idleCheckInterval {
		check = idleCheckInterval
	}

	s.idleWg.Add(1)
	go func() {
		defer s.idleWg.Done()

		ticker := time.NewTicker(check)
		defer ticker.Stop()

		for {
			select {
			case <-s.idleDone:
				return
			case <-ticker.C:
				s.triggerIdle(interval)
			}
		}
	}()

	logger.Infof("主动发言已启用: 静默超过 %s 说一句", interval)
}

// triggerIdle 找出够久没动静的渠道，让它们各自主动说一句。
func (s *Sessions) triggerIdle(interval time.Duration) {
	s.mu.Lock()
	targets := make([]*session, 0, len(s.byChannel))
	for _, item := range s.byChannel {
		active := time.Unix(0, item.lastActive.Load())
		idle := time.Unix(0, item.lastIdle.Load())

		// 距离上次真实事件够久，且这段时间里还没主动说过
		if time.Since(active) < interval || !idle.Before(active) {
			continue
		}
		item.lastIdle.Store(time.Now().UnixNano())
		targets = append(targets, item)
	}
	s.mu.Unlock()

	for _, item := range targets {
		go item.speakIdle()
	}
}

// speakIdle 让一个会话主动说一句，入播报队列，最低优先级。
func (s *session) speakIdle() {
	if s.cfg.Broadcast == nil {
		return
	}

	prompt := s.cfg.IdleSpeakPrompt
	if strings.TrimSpace(prompt) == "" {
		prompt = defaultIdlePrompt
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.TurnTimeout)
	defer cancel()

	var reply strings.Builder
	// 主动发言用 Chat 而不是把提示词写进历史：这是一次性指令，不是用户说的话
	err := s.agent.ChatWithContext(ctx, prompt, "", func(chunk string) error {
		reply.WriteString(chunk)
		return nil
	})
	if err != nil {
		logger.Warnf("渠道 %s 主动发言失败: %v", s.channel, err)
		return
	}

	text := strings.TrimSpace(reply.String())
	if text == "" {
		return
	}

	item := broadcast.Item{
		Priority: broadcast.PriorityIdle,
		Text:     text,
		Source:   "idle:" + s.channel,
	}
	if !s.cfg.Broadcast.Enqueue(item) {
		logger.Warnf("渠道 %s 主动发言被队列拒绝", s.channel)
		return
	}
	logger.Infof("渠道 %s 主动发言已入队: %s", s.channel, text)
}
