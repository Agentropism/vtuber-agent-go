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

	"github.com/Agentropism/vtuber-agent-go/internal/logger"
)

// streamRuntime 是推流链路：开播取地址 → 虚拟屏渲染 → ffmpeg 推上去。
//
// ffmpeg、Xvfb、Chrome 都在 Run 里才启动：推流是长任务，装配阶段只准备配置，
// 这样配置写错时能在启动阶段报错，而不是开了播才发现推不上去。
type streamRuntime struct {
	cfg      config.StreamConfig
	live     stream.LiveConfig
	renderer *stream.Renderer
	// loginURL 是扫码登录页地址，只在「未登录」提示里用一次。
	loginURL string
	// verifyURL 是开播验证页地址（60024 扫码 / 60043 人脸）。
	verifyURL string

	mu       sync.Mutex
	streamer *stream.Streamer

	// playing 计数在播报期间不为 0：保活补静音必须让位，否则会把音频时间线撑长
	playing atomic.Int32

	// streaming 表示 ffmpeg 正在推；pending 是「需要用户在手机上做一步」的状态。
	// 两者都受 mu 保护，供 /login/verify 读取。
	streaming bool
	pending   *pendingVerify
}

// pendingVerify 是最近一次开播尝试的失败状态。
//
// 不只是「需要用户去手机做一步」：像 60045（账号准入）这种没有手机动作可做的拒绝
// 也要留在页面上——不然用户打开验证页只会看到「还没有开播记录」，比日志还不如。
type pendingVerify struct {
	Code     int
	Message  string
	QR       string // 60024：用 B 站 App 扫这个地址
	FaceAuth string // 60043：实名/人脸认证页
	At       time.Time
}

// streamStatus 是推流的当前状态，供验证页展示。
type streamStatus struct {
	Streaming bool
	Pending   *pendingVerify
}

// Status 返回当前推流状态。
func (r *streamRuntime) Status() streamStatus {
	r.mu.Lock()
	defer r.mu.Unlock()

	return streamStatus{Streaming: r.streaming, Pending: r.pending}
}

// setPending 记录最近一次开播失败（含账号准入这类没有手机动作可做的拒绝）。
func (r *streamRuntime) setPending(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err == nil {
		r.pending = nil

		return
	}

	// 非 StartLiveError 的失败（网络、liveVersion 取不到等）也要显示，只是没有业务码
	r.pending = &pendingVerify{At: time.Now(), Message: err.Error()}

	var liveErr *stream.StartLiveError
	if errors.As(err, &liveErr) {
		r.pending.Code = liveErr.Code
		r.pending.Message = liveErr.Message
		r.pending.QR = liveErr.QR
		r.pending.FaceAuth = liveErr.FaceAuth
	}
}

// needsUserAction 判断这次失败是不是「等用户在手机上扫码/刷脸」这一类。
func needsUserAction(err error) bool {
	var liveErr *stream.StartLiveError

	return errors.As(err, &liveErr) && liveErr.NeedsUserAction()
}

// provideStream 装配推流链路；未启用 [stream] 时返回 nil。
func provideStream(cfg *config.Config) (*streamRuntime, error) {
	s := cfg.Stream
	if !s.Enabled {
		logger.Info("未启用 [stream]，跳过推流")
		return nil, nil
	}
	// 推流地址只有两个来源：手填 output，或开播接口拿。开播要 room_id，
	// 登录态可以等扫码登录后再补（见 waitForCookie）。
	if strings.TrimSpace(s.Output) == "" && s.RoomID == 0 {
		return nil, errors.New("[stream] 已启用，但既没有 output 也没有 room_id：不知道往哪推")
	}

	runtime := &streamRuntime{
		cfg:       s,
		live:      stream.LiveConfig{RoomID: s.RoomID, AreaID: s.AreaID},
		loginURL:  loginURL(cfg.Server.Addr),
		verifyURL: verifyURL(cfg.Server.Addr),
	}

	// renderer 的零值是 false，最容易配漏：screen 模式又不自备显示时，ffmpeg 会以
	// 「Cannot open display」告终，那句话离真正的原因（少写一行 renderer = true）很远。
	if !s.Renderer && s.Input != string(stream.InputTest) {
		logger.Warnf("未开启 [stream].renderer：需要自备 X 显示 %s（本进程不会起 Xvfb/Chrome）", s.Display)
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
	return hostURL(addr) + "/web/?autostart=1"
}

// loginURL 是扫码登录页地址。
func loginURL(addr string) string {
	return hostURL(addr) + loginPagePattern
}

// verifyURL 是开播验证页地址（60024 扫码 / 60043 人脸）。
func verifyURL(addr string) string {
	return hostURL(addr) + loginVerifyPattern
}

// hostURL 把监听地址换成浏览器能访问的地址：":8080" → "http://127.0.0.1:8080"。
//
// 监听 0.0.0.0 时不能照抄给浏览器；登录页本来就只允许本机访问，用回环地址最稳。
func hostURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "", strings.TrimPrefix(addr, ":")
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}

	return fmt.Sprintf("http://%s:%s", host, port)
}

// Run 持续维持推流：开播这一步失败会重试，退避由短到长。
//
// 与 streamer 自己的重启分工不同：那一层管 ffmpeg 中途掉线，这一层管「开播没成」。
// 没有它，一次风控、一次开播验证、一次网络抖动就会让推流永久停摆（实测被 60045 卡过）。
func (r *streamRuntime) Run(ctx context.Context) error {
	backoff := startRetryInitial

	for {
		started, err := r.runOnce(ctx)

		if ctx.Err() != nil {
			return ctx.Err()
		}

		wait := backoff
		if started {
			// 开播成功过又退出来是运行期故障：退避重置，别背着旧账
			backoff = startRetryInitial
			wait = startRetryInitial
		} else {
			backoff = min(backoff*2, startRetryMax)
		}
		if needsUserAction(err) {
			// 用户正在手机上扫码/刷脸，等短一点好接上
			wait = verifyRetryInterval
		}

		logger.Warnf("推流未建立，%s 后重试: %v", wait, err)
		if needsUserAction(err) {
			logger.Infof("需要你在手机上完成验证，完成后会自动继续: %s", r.verifyURL)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// runOnce 走一遍完整链路：开播 → 起画面 → 推流；返回是否成功开播过。
func (r *streamRuntime) runOnce(ctx context.Context) (bool, error) {
	output, started, err := r.resolveOutput(ctx)
	if err != nil {
		return false, err
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
		RestartWait:  r.cfg.RestartWait.Std(),
	})
	if err != nil {
		return started, err
	}
	defer streamer.Close()

	r.mu.Lock()
	r.streamer = streamer
	r.streaming = true
	r.pending = nil
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.streamer = nil
		r.streaming = false
		r.mu.Unlock()
	}()

	// 画面先起来再推：抓一块还没画出来的屏，前几秒是黑的
	if r.renderer != nil {
		if err := r.renderer.Start(ctx); err != nil {
			return started, err
		}
		defer r.renderer.Stop()
	}

	// 音频保活与 ffmpeg 同时起停
	go r.silenceKeepalive(ctx)

	return started, streamer.Run(ctx)
}

// resolveOutput 决定推流地址：给了 output 就用它，否则调开播接口拿。
func (r *streamRuntime) resolveOutput(ctx context.Context) (output string, started bool, err error) {
	if strings.TrimSpace(r.cfg.Output) != "" {
		logger.Info("使用 [stream].output 指定的推流地址（不调开播接口）")
		return r.cfg.Output, false, nil
	}

	// 登录页由本进程提供，首次使用的顺序必然是「先起服务 → 扫码 → 推流」，
	// 所以这里等凭据，而不是直接失败。
	cookie, err := r.waitForCookie(ctx)
	if err != nil {
		return "", false, err
	}

	live := r.live
	live.Cookie = cookie

	info, err := stream.StartLive(ctx, live)
	if err != nil {
		r.setPending(err)

		return "", false, err
	}
	r.setPending(nil)
	logger.Infof("已开播: 直播间 %d", live.RoomID)

	return info.Output(), true, nil
}

// cookieWaitInterval 是等待扫码登录的轮询间隔。
const cookieWaitInterval = 5 * time.Second

// 开播阶段失败的重试退避：从 30 秒起翻倍，5 分钟封顶。
const (
	startRetryInitial = 30 * time.Second
	startRetryMax     = 5 * time.Minute
	// verifyRetryInterval 是「等用户在手机上扫码/刷脸」时的重试间隔。
	verifyRetryInterval = 15 * time.Second
)

// waitForCookie 要一个可用的登录态：配置里填了就用它，否则读扫码登录落盘的文件。
//
// 登录页由本进程提供，所以首次使用的顺序必然是「先起服务 → 绕去 /login/ 扫码 → 推流」，
// 这里必须等而不是直接失败；等到之前每 5 秒重试一次，并在第一次就给出可点的地址。
func (r *streamRuntime) waitForCookie(ctx context.Context) (string, error) {
	if cookie := strings.TrimSpace(r.cfg.Cookie); cookie != "" {
		logger.Info("使用 [stream].cookie 提供的登录态")

		return cookie, nil
	}

	path := cookiePath(r.cfg)
	warned := false
	for {
		if cookie := stream.LoadLoginCookie(path); cookie != "" {
			logger.Infof("使用扫码登录保存的登录态: %s", path)

			return cookie, nil
		}

		if !warned {
			logger.Warnf("未登录：浏览器打开 %s 扫码，或直接填 [stream].cookie（每 %s 重试一次）", r.loginURL, cookieWaitInterval)
			warned = true
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cookieWaitInterval):
		}
	}
}

// stopLive 关播。用独立 ctx：走到这里时主 ctx 已经取消了。
func (r *streamRuntime) stopLive() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := stream.StopLive(ctx, r.live); err != nil {
		logger.Warnf("关播失败（直播间可能仍显示直播中）: %v", err)
		return
	}
	logger.Info("已关播")
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
}

func newStreamSink(runtime *streamRuntime) *streamSink {
	return &streamSink{runtime: runtime}
}

// streamChunkMillis 是每块 PCM 的时长：50ms 够平滑，也不会把等待切得太碎。
const streamChunkMillis = 50

func (s *streamSink) Play(ctx context.Context, item broadcast.Item, pcm []byte) error {
	// 播报期间不许补静音（见 silenceKeepalive）
	s.runtime.playing.Add(1)
	defer s.runtime.playing.Add(-1)

	seconds := float64(len(pcm)/tts.BytesPerSample) / float64(tts.SampleRate)
	logger.Infof("[推流] priority=%s source=%s 时长=%.2fs 文本=%s",
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
