// Package distillery 将直播高优先级互动转发给语音反馈服务。
package distillery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// UnifiedEvent 是 llm-vup-bridge /event 端点接收的统一事件结构。
type UnifiedEvent struct {
	Platform    string `json:"platform"`
	UserID      string `json:"user_id"`
	UserName    string `json:"user_name"`
	GroupID     string `json:"group_id"`
	Content     string `json:"content"`
	MessageType string `json:"message_type"`
}

var (
	log        = zap.NewNop()
	httpClient = &http.Client{Timeout: 3 * time.Second}
	targetMu   sync.RWMutex
	targetURL  string
)

// SetLogger 注入日志实例。
func SetLogger(logger *zap.Logger) {
	if logger != nil {
		log = logger
	}
}

// Init 设置 distillery 的完整事件接收地址；空字符串表示禁用转发。
func Init(target string) {
	targetMu.Lock()
	targetURL = target
	targetMu.Unlock()
}

// Post 异步转发事件；网络失败仅记录日志，不阻塞事件处理链。
func Post(event UnifiedEvent) {
	targetMu.RLock()
	target := targetURL
	targetMu.RUnlock()
	if target == "" {
		log.Sugar().Debug("distillery 目标地址为空，跳过事件转发")
		return
	}

	go func() {
		if err := send(target, event); err != nil {
			log.Sugar().Warnf("转发 distillery 事件失败: %v", err)
		}
	}()
}

func send(target string, event UnifiedEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化 distillery 事件: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建 distillery 请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求 distillery: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("distillery 返回非成功状态: %s", resp.Status)
	}
	return nil
}
