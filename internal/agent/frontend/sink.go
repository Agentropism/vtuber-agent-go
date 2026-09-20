package frontend

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/agent/broadcast"

	"github.com/Agentropism/vtuber-agent-go/internal/logger"
	"github.com/Agentropism/vtuber-agent-go/internal/shared/emotion"
)

// playbackGrace 是等待前端回执时在音频时长之外额外给的宽限。
const playbackGrace = 5 * time.Second

// Sink 把播报队列的内容送到浏览器。
//
// 它实现了 broadcast.Sink：Play 返回即表示这一段播报结束，队列才会继续下一句。
// 因此这里会等前端回执——不这样的话，队列会按「TTS 合成完」而不是「放完了」推进，
// 多句连播就会叠在一起。
type Sink struct {
	hub      *hub
	emotions *emotion.Map
	sequence atomic.Uint64
}

// NewSink 构造播报投递实现。
func NewSink(h *hub, emotions *emotion.Map) *Sink {
	return &Sink{hub: h, emotions: emotions}
}

// speakData 是下发给前端的播报内容。
type speakData struct {
	Seq      uint64 `json:"seq"`
	Text     string `json:"text"`
	Emotion  int    `json:"emotion"` // Live2D 表达式下标，-1 表示不换表情
	Audio    string `json:"audio"`   // base64 WAV；为空表示静音字幕
	Priority string `json:"priority"`
	Source   string `json:"source"`
	Duration int    `json:"duration_ms"`
}

// Play 把一段播报送到所有前端，并等它播完。
func (s *Sink) Play(ctx context.Context, item broadcast.Item, pcm []byte) error {
	if s.hub.count() == 0 {
		logger.Debugf("没有前端连接，跳过播报: %s", item.Text)
		return nil
	}

	duration := audioDuration(pcm)
	seq := s.sequence.Add(1)

	waiter := s.hub.registerWaiter(seq)
	defer s.hub.unregisterWaiter(seq)

	sent := s.hub.broadcast(ctx, message{
		Type: "speak",
		Data: speakData{
			Seq:      seq,
			Text:     item.Text,
			Emotion:  s.expression(item),
			Audio:    encodeAudio(pcm),
			Priority: item.Priority.String(),
			Source:   item.Source,
			Duration: int(duration.Milliseconds()),
		},
	})
	if sent == 0 {
		logger.Warnf("播报没有送达任何前端: %s", item.Text)
		return nil
	}

	// 等前端播完；超时上限按音频时长放宽，避免前端不回执时把队列卡死。
	timeout := duration + playbackGrace
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		// 被更高优先级打断：队列要求尽快返回，浏览器那边下一句会顶掉这一句
		return ctx.Err()
	case <-waiter:
		return nil
	case <-timer.C:
		logger.Warnf("等待前端播完超时（%s）: %s", timeout.Round(time.Millisecond), item.Text)
		return nil
	}
}

// expression 把条目上的表情算成 Live2D 表达式下标。
//
// 优先用 Item.Emotion（注入路径显式给出的标签名），没有就按「无表情」处理；
// 文本里的 [joy] 这类标签早在入队前就被剥离并写进了 Item.Emotion。
func (s *Sink) expression(item broadcast.Item) int {
	if item.Emotion == "" {
		return -1
	}

	index, ok := s.emotions.Index(item.Emotion)
	if !ok {
		logger.Warnf("表情标签不在当前模型的 emo_map 里，忽略: %s", item.Emotion)
		return -1
	}

	return index
}
