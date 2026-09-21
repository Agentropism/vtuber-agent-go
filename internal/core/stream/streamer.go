// Package stream 提供直播推流（把画面与声音推到 RTMP，替代 OBS 的推流职责）。
//
// 设计前提（都经过实测确认）：
//
//   - /api/client-ws 上没有视频，只有文本、表情下标与 base64 WAV。所以「把 ws 转成
//     推流格式」最多得到音频轨与字幕，人物画面必须有人渲染。
//   - 本机是 Wayland 会话，ffmpeg 的 x11grab 抓不到原生窗口（实测抓出来近全黑），
//     而 ffmpeg 不支持 xdg-desktop-portal 那套 Wayland 抓屏。
//
// 因此画面走「虚拟 X 屏」这条路：
//
//	Xvfb 虚拟屏 :99 → Chrome 全屏跑 /web/ 页面 → ffmpeg x11grab 抓这块屏
//	播报队列合成出的裸 PCM → 命名管道 → ffmpeg 编 AAC
//	两路合成 → libx264 + aac → flv → rtmp
//
// 对使用者来说这就等于「不需要前端」：不用开浏览器窗口、不用手点开始，
// 整条链可以在没有图形界面的机器上无人值守跑。渲染仍然由 Chromium 完成——
// 自己实现 Cubism 渲染器需要 CGo 与原生 SDK，与本项目「无 CGo、单二进制」冲突。
package stream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
)

// 默认参数。
const (
	defaultFFmpeg     = "ffmpeg"
	defaultDisplay    = ":99"
	defaultWidth      = 1280
	defaultHeight     = 720
	defaultFPS        = 30
	defaultVideoRate  = "2500k"
	defaultAudioRate  = "128k"
	defaultPreset     = "veryfast"
	defaultSampleRate = 24000
	// audioChannels 是写进管道的声道数：tts 契约是单声道。
	audioChannels      = 1
	defaultKeyInterval = 60
	defaultRestartWait = 5 * time.Second

	// audioFifoName 是音频管道文件名。
	audioFifoName = "audio.fifo"
)

// InputKind 是画面来源。
type InputKind string

const (
	// InputScreen 抓 X 显示（生产路径：Xvfb 虚拟屏上的 Chrome）。
	InputScreen InputKind = "screen"
	// InputTest 用 ffmpeg 自带测试画面，用于自检（不需要 Xvfb 与浏览器）。
	InputTest InputKind = "test"
)

// Config 是推流配置。
type Config struct {
	// Output 是推流地址，形如 rtmp://.../live-bvc/?streamname=...&key=...
	// 也可以给本地文件路径（.flv），便于自检。
	Output string
	// Input 默认 InputScreen。
	Input InputKind
	// Display 是 X 显示号，默认 :99（由 Xvfb 提供）。
	Display string

	FFmpeg string // ffmpeg 可执行文件，默认 "ffmpeg"

	Width  int // 默认 1280
	Height int // 默认 720
	FPS    int // 默认 30

	VideoBitrate string // 默认 2500k
	AudioBitrate string // 默认 128k
	// Encoder 默认 libx264；有 N 卡可换 h264_nvenc。
	Encoder string

	// 写进管道的 PCM 格式固定为 24kHz 单声道（tts 契约），不开放配置。

	// RestartWait 是 ffmpeg 异常退出后的重启间隔，默认 5s；<0 表示不重启。
	RestartWait time.Duration
}

func (c Config) withDefaults() Config {
	if c.FFmpeg == "" {
		c.FFmpeg = defaultFFmpeg
	}
	if c.Input == "" {
		c.Input = InputScreen
	}
	if c.Display == "" {
		c.Display = defaultDisplay
	}
	if c.Width <= 0 {
		c.Width = defaultWidth
	}
	if c.Height <= 0 {
		c.Height = defaultHeight
	}
	if c.FPS <= 0 {
		c.FPS = defaultFPS
	}
	if c.VideoBitrate == "" {
		c.VideoBitrate = defaultVideoRate
	}
	if c.AudioBitrate == "" {
		c.AudioBitrate = defaultAudioRate
	}
	if c.Encoder == "" {
		c.Encoder = "libx264"
	}
	if c.RestartWait == 0 {
		c.RestartWait = defaultRestartWait
	}

	return c
}

// Streamer 把虚拟屏上的画面与队列产出的 PCM 交给 ffmpeg 推出去。
type Streamer struct {
	cfg Config

	// audioPath 在构造时就定下来：参数组装与写入必须指向同一个管道，
	// 两边各算一次路径是这类实现最容易出错的地方。
	dir       string
	audioPath string

	mu      sync.Mutex
	running bool
}

// New 构造推流器。
func New(cfg Config) (*Streamer, error) {
	cfg = cfg.withDefaults()

	if strings.TrimSpace(cfg.Output) == "" {
		return nil, errors.New("stream: 未配置推流地址")
	}
	if cfg.Input != InputScreen && cfg.Input != InputTest {
		return nil, fmt.Errorf("stream: 未知的画面来源 %q", cfg.Input)
	}
	if _, err := exec.LookPath(cfg.FFmpeg); err != nil {
		return nil, fmt.Errorf("stream: 找不到 %s: %w", cfg.FFmpeg, err)
	}

	dir, err := os.MkdirTemp("", "syagent-stream-*")
	if err != nil {
		return nil, fmt.Errorf("stream: 创建管道目录: %w", err)
	}
	audioPath := filepath.Join(dir, audioFifoName)
	if err := syscall.Mkfifo(audioPath, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("stream: 创建音频管道: %w", err)
	}

	return &Streamer{cfg: cfg, dir: dir, audioPath: audioPath}, nil
}

// Close 删掉管道目录。
func (s *Streamer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" {
		return nil
	}
	err := os.RemoveAll(s.dir)
	s.dir = ""
	s.audioPath = ""

	return err
}

// Args 返回给 ffmpeg 的参数。
//
// 单独暴露是为了能在不启动进程的情况下断言参数组装：推流参数写错往往要等到
// 开播才发现，这里先把它钉在测试里。
func (s *Streamer) Args() []string {
	cfg := s.cfg

	// -y 必须有：推流中途失败会在输出路径留下半截文件，没有它 ffmpeg 会直接
	// 「File already exists. Exiting.」——重启与重试就永远起不来了（实测踩到）。
	args := []string{"-hide_banner", "-loglevel", "warning", "-nostdin", "-y"}

	// 视频输入
	if cfg.Input == InputTest {
		args = append(args,
			"-f", "lavfi",
			"-i", fmt.Sprintf("testsrc=size=%dx%d:rate=%d", cfg.Width, cfg.Height, cfg.FPS),
		)
	} else {
		args = append(args,
			"-f", "x11grab",
			"-video_size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
			"-framerate", strconv.Itoa(cfg.FPS),
			"-i", cfg.Display,
		)
	}

	// 音频输入：命名管道，由 WritePCM 写入
	args = append(args,
		"-f", "s16le",
		"-ar", strconv.Itoa(defaultSampleRate),
		"-ac", strconv.Itoa(audioChannels),
		"-i", s.audioPath,
	)

	args = append(args,
		"-c:v", cfg.Encoder,
		"-preset", defaultPreset,
		"-b:v", cfg.VideoBitrate,
		"-maxrate", cfg.VideoBitrate,
		"-bufsize", cfg.VideoBitrate,
		"-pix_fmt", "yuv420p", // 直播要求的兼容格式
		"-g", strconv.Itoa(defaultKeyInterval),

		"-c:a", "aac",
		"-b:a", cfg.AudioBitrate,
		"-ar", "44100",
		"-ac", "2",

		"-f", "flv",
		cfg.Output,
	)

	return args
}

// Run 启动 ffmpeg 并阻塞到 ctx 取消；进程异常退出时按配置重试。
func (s *Streamer) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("stream: 已经在推流")
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	for {
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			logger.Info("推流已停止")
			return ctx.Err()
		}
		if s.cfg.RestartWait < 0 {
			return err
		}

		logger.Warnf("推流中断: %v；%s 后重试", err, s.cfg.RestartWait)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.cfg.RestartWait):
		}
	}
}

// runOnce 起一次 ffmpeg 并等它结束。
func (s *Streamer) runOnce(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, s.cfg.FFmpeg, s.Args()...)
	cmd.Stderr = &logWriter{name: "ffmpeg"}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg: %w", err)
	}
	logger.Infof("推流已启动: %dx%d@%dfps %s → %s",
		s.cfg.Width, s.cfg.Height, s.cfg.FPS, s.cfg.Encoder, clipURL(s.cfg.Output))

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		return fmt.Errorf("ffmpeg 退出: %w", waitErr)
	}

	return errors.New("ffmpeg 正常退出（推流地址可能拒绝了连接）")
}

// WritePCM 写一段 s16le PCM。
//
// 管道没有读端（ffmpeg 还没起来或已经退出）时直接丢弃，绝不阻塞调用方——
// 推流不该把播报链路拖死。
func (s *Streamer) WritePCM(pcm []byte) {
	s.mu.Lock()
	path := s.audioPath
	s.mu.Unlock()

	if len(pcm) == 0 || path == "" {
		return
	}

	file, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return // 没有读端
	}
	defer file.Close()

	_, _ = file.Write(pcm)
}

// logWriter 把子进程的 stderr 接进本进程的日志。
//
// name 必须按进程给：三个子进程（Xvfb / Chrome / ffmpeg）共用这个类型，
// 早先写死 "ffmpeg:" 前缀，Chrome 的 GPU 报错被记成 ffmpeg 的，排查时误导过一轮。
type logWriter struct{ name string }

func (w *logWriter) Write(p []byte) (int, error) {
	if text := strings.TrimSpace(string(p)); text != "" {
		logger.Debugf("%s: %s", w.name, text)
	}

	return len(p), nil
}

// clipURL 隐去推流地址里的密钥，日志与错误里都不能出现它。
func clipURL(url string) string {
	if before, _, found := strings.Cut(url, "key="); found {
		return before + "key=***"
	}

	return url
}
