package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/agent/broadcast"
	"github.com/Agentropism/vtuber-agent-go/internal/config"
	"github.com/Agentropism/vtuber-agent-go/internal/stream"
	"github.com/Agentropism/vtuber-agent-go/internal/tts"

	"go.uber.org/zap"
)

// streamRuntime 是推流链路：开播取地址 → 虚拟屏渲染 → ffmpeg 推上去。
//
// ffmpeg、Xvfb、Chrome 都在 Run 里才启动：推流是长任务，装配阶段只准备配置，
// 这样配置写错时能在启动阶段报错，而不是开了播才发现推不上去。
type streamRuntime struct {
	cfg      config.StreamConfig
	live     stream.LiveConfig
	renderer *stream.Renderer
	log      *zap.Logger

	mu       sync.Mutex
	streamer *stream.Streamer

	// playing 计数在播报期间不为 0：保活补静音必须让位，否则会把音频时间线撑长
	playing atomic.Int32
}

// provideStream 装配推流链路；未启用 [stream] 时返回 nil。
func provideStream(cfg *config.Config, log *zap.Logger) (*streamRuntime, error) {
	s := cfg.Stream
	if !s.Enabled {
		log.Sugar().Info("未启用 [stream]，跳过推流")
		return nil, nil
	}
	// 推流地址只有两个来源：手填 output，或开播接口拿。
	if strings.TrimSpace(s.Output) == "" && (strings.TrimSpace(s.Cookie) == "" || s.RoomID == 0) {
		return nil, errors.New("[stream] 已启用，但既没有 output 也没有 cookie + room_id：不知道往哪推")
	}

	stream.SetLogger(log)

	runtime := &streamRuntime{
		cfg:  s,
		live: stream.LiveConfig{Cookie: s.Cookie, RoomID: s.RoomID, AreaID: s.AreaID},
		log:  log,
	}

	// input = test 用的是 ffmpeg 自带测试画面，不需要虚拟屏与浏览器
	if s.Renderer && s.Input != string(stream.InputTest) {
		renderer, err := stream.NewRenderer(stream.RendererConfig{
			Display: s.Display,
			Xvfb:    s.Xvfb,
			Chrome:  s.Chrome,
			URL:     webPageURL(cfg.Server.Addr),
			Width:   s.Width,
			Height:  s.Height,
		})
		if err != nil {
			return nil, err
		}
		runtime.renderer = renderer
	}

	return runtime, nil
}

// webPageURL 拼出让 Chrome 打开的页面地址：":8080" → http://127.0.0.1:8080/web/?autostart=1
//
// autostart=1 是给无人值守用的：虚拟屏上没有人能点「点击开始」。
func webPageURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "", strings.TrimPrefix(addr, ":")
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}

	return fmt.Sprintf("http://%s:%s/web/?autostart=1", host, port)
}

// Run 解析推流地址、起画面与 ffmpeg，阻塞到 ctx 取消。
func (r *streamRuntime) Run(ctx context.Context) error {
	output, started, err := r.resolveOutput(ctx)
	if err != nil {
		return err
	}
	if started {
		defer r.stopLive()
	}

	streamer, err := stream.New(stream.Config{
		Output:       output,
		Input:        stream.InputKind(r.cfg.Input),
		Display:      r.cfg.Display,
		FFmpeg:       r.cfg.FFmpeg,
		Width:        r.cfg.Width,
		Height:       r.cfg.Height,
		FPS:          r.cfg.FPS,
		VideoBitrate: r.cfg.VideoBitrate,
		AudioBitrate: r.cfg.AudioBitrate,
		Encoder:      r.cfg.Encoder,
		RestartWait:  r.cfg.RestartWait,
	})
	if err != nil {
		return err
	}
	defer streamer.Close()

	r.mu.Lock()
	r.streamer = streamer
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.streamer = nil
		r.mu.Unlock()
	}()

	// 画面先起来再推：抓一块还没画出来的屏，前几秒是黑的
	if r.renderer != nil {
		if err := r.renderer.Start(ctx); err != nil {
			return err
		}
		defer r.renderer.Stop()
	}

	// 音频保活与 ffmpeg 同时起停
	go r.silenceKeepalive(ctx)

	return streamer.Run(ctx)
}

// resolveOutput 决定推流地址：给了 output 就用它，否则调开播接口拿。
func (r *streamRuntime) resolveOutput(ctx context.Context) (output string, started bool, err error) {
	if strings.TrimSpace(r.cfg.Output) != "" {
		r.log.Sugar().Info("使用 [stream].output 指定的推流地址（不调开播接口）")
		return r.cfg.Output, false, nil
	}

	info, err := stream.StartLive(ctx, r.live)
	if err != nil {
		return "", false, err
	}
	r.log.Sugar().Infof("已开播: 直播间 %d", r.live.RoomID)

	return info.Output(), true, nil
}

// stopLive 关播。用独立 ctx：走到这里时主 ctx 已经取消了。
func (r *streamRuntime) stopLive() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := stream.StopLive(ctx, r.live); err != nil {
		r.log.Sugar().Warnf("关播失败（直播间可能仍显示直播中）: %v", err)
		return
	}
	r.log.Sugar().Info("已关播")
}

// Close 收尾渲染器（推流管道由 Run 的 defer 关）。
func (r *streamRuntime) Close() {
	if r.renderer != nil {
		r.renderer.Stop()
	}
}

// writePCM 把一段 PCM 写进推流管道；还没开始推流时直接丢弃。
func (r *streamRuntime) writePCM(pcm []byte) {
	r.mu.Lock()
	streamer := r.streamer
	r.mu.Unlock()
	if streamer == nil {
		return
	}

	streamer.WritePCM(pcm)
}

// silenceKeepalive 在没人说话时给管道补静音。
//
// 非要它不可的原因：命名管道没有写端时 ffmpeg 会阻塞在读音频上，连视频一起停。
// 「没配 TTS」和「长时间没人说话」都会撞上这个，表现是推流看着连着、画面却不动。
//
// 播报进行中（playing > 0）必须让位：管道里的数据就是音频时间线，插静音等于把
// 语音拉长，和画面不同步。
func (r *streamRuntime) silenceKeepalive(ctx context.Context) {
	chunk := tts.SampleRate * tts.BytesPerSample * silenceChunkMillis / 1000
	if chunk <= 0 {
		return
	}
	silence := make([]byte, chunk)

	ticker := time.NewTicker(time.Duration(silenceChunkMillis) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if r.playing.Load() > 0 {
			continue
		}
		r.writePCM(silence)
	}
}

// silenceChunkMillis 是每次补静音的时长，与补的节奏一致才是实时速率。
const silenceChunkMillis = 100

// streamSink 把播报音频喂给推流管道。
//
// 它按音频时长分小块写，而不是一次灌完整段：推流是实时的，管道写端非阻塞，
// 一次写超出管道缓冲的部分会被直接丢掉（听感上就是缺字）。分块 + 按时长等待
// 同时让播报队列的抢占与冷却保持真实时序。
//
// 注意它不替代前端 Sink：Chrome 里那个页面仍然要靠 /client-ws 的 speak 驱动
// 口型与字幕，所以两者是并行投递（见 fanOutSink）。
type streamSink struct {
	runtime *streamRuntime
	log     *zap.Logger
}

func newStreamSink(runtime *streamRuntime, log *zap.Logger) *streamSink {
	return &streamSink{runtime: runtime, log: log}
}

// streamChunkMillis 是每块 PCM 的时长：50ms 够平滑，也不会把等待切得太碎。
const streamChunkMillis = 50

func (s *streamSink) Play(ctx context.Context, item broadcast.Item, pcm []byte) error {
	// 播报期间不许补静音（见 silenceKeepalive）
	s.runtime.playing.Add(1)
	defer s.runtime.playing.Add(-1)

	seconds := float64(len(pcm)/tts.BytesPerSample) / float64(tts.SampleRate)
	s.log.Sugar().Infof("[推流] priority=%s source=%s 时长=%.2fs 文本=%s",
		item.Priority, item.Source, seconds, item.Text)

	chunk := tts.SampleRate * tts.BytesPerSample * streamChunkMillis / 1000
	if chunk <= 0 {
		chunk = len(pcm)
	}

	for offset := 0; offset < len(pcm); offset += chunk {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := min(offset+chunk, len(pcm))
		s.runtime.writePCM(pcm[offset:end])

		frames := float64((end - offset) / tts.BytesPerSample)
		wait := time.Duration(frames / float64(tts.SampleRate) * float64(time.Second))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}

	return nil
}
