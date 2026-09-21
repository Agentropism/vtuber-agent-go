package stream

import (
	"context"
	"errors"
	"fmt"
	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 默认渲染器参数。
const (
	defaultXvfb   = "Xvfb"
	defaultChrome = "google-chrome-stable"
	// displayXDepth 是虚拟屏色深；24 位足够 x264 用，8 位会掉色。
	displayXDepth = 24
	// displayReadyTimeout 是等 X socket 出现的上限。
	displayReadyTimeout = 10 * time.Second
	// displayPollInterval 是轮询 X socket 的间隔。
	displayPollInterval = 200 * time.Millisecond
	// displayAliveTimeout 是判断 X socket 是否真活着时的连接超时。
	displayAliveTimeout = 200 * time.Millisecond
	// chromeWarmup 是 Chrome 起来到画面稳定之间的等待。
	//
	// 这个值只能靠等：Xvfb 出 socket 不等于画面已经画出来，而「页面加载完」
	// 没有不依赖浏览器扩展的可观测信号。等短了前几秒是黑屏，等长了浪费启动时间，
	// 3 秒是实测够用的初值，机器慢可直接把 RendererConfig.Warmup 调大。
	defaultChromeWarmup = 3 * time.Second
	// killGrace 是停进程时从 SIGTERM 到 SIGKILL 的等待。
	killGrace = 5 * time.Second
)

// RendererConfig 是虚拟屏与浏览器的启动参数。
type RendererConfig struct {
	// Display 是 X 显示号，默认 :99。
	Display string
	// Xvfb 与 Chrome 是可执行文件名，默认 "Xvfb" 与 "google-chrome-stable"。
	Xvfb   string
	Chrome string
	// URL 是页面地址，必填，形如 http://127.0.0.1:6199/?autostart=1。
	URL string
	// Width / Height 是虚拟屏尺寸，需与推流尺寸一致，否则画面会缩放。
	Width  int
	Height int
	// Warmup 是 Chrome 启动后的等待，<=0 用默认值。
	Warmup time.Duration
}

// Renderer 起一块虚拟 X 屏并在上面全屏跑前端页面。
//
// 画面必须有人渲染：/api/client-ws 上只有文本、表情与音频，没有视频。这里用
// Xvfb + Chrome 当渲染器，ffmpeg 再从这块屏上抓画面（见包注释）。
//
// 已经有一块同号的显示存在时不会重复起 Xvfb，也不会在 Stop 时把别人的屏关掉。
type Renderer struct {
	cfg RendererConfig

	mu         sync.Mutex
	xvfb       *exec.Cmd
	chrome     *exec.Cmd
	profileDir string
	ownsXvfb   bool
	started    bool
}

// NewRenderer 构造渲染器，只校验参数与可执行文件，不启动任何进程。
func NewRenderer(cfg RendererConfig) (*Renderer, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("stream: 渲染器未配置页面地址")
	}
	if cfg.Display == "" {
		cfg.Display = defaultDisplay
	}
	if cfg.Xvfb == "" {
		cfg.Xvfb = defaultXvfb
	}
	if cfg.Chrome == "" {
		cfg.Chrome = defaultChrome
	}
	if cfg.Width <= 0 {
		cfg.Width = defaultWidth
	}
	if cfg.Height <= 0 {
		cfg.Height = defaultHeight
	}
	if cfg.Warmup <= 0 {
		cfg.Warmup = defaultChromeWarmup
	}
	if _, err := exec.LookPath(cfg.Xvfb); err != nil {
		return nil, fmt.Errorf("stream: 找不到 %s: %w", cfg.Xvfb, err)
	}
	if _, err := exec.LookPath(cfg.Chrome); err != nil {
		return nil, fmt.Errorf("stream: 找不到 %s: %w", cfg.Chrome, err)
	}

	return &Renderer{cfg: cfg}, nil
}

// Display 返回 X 显示号。
func (r *Renderer) Display() string { return r.cfg.Display }

// Start 起 Xvfb（显示屏不存在时）与 Chrome，返回时画面已经等过暖机时间。
func (r *Renderer) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return errors.New("stream: 渲染器已经启动")
	}

	if displayAlive(r.cfg.Display) {
		logger.Infof("显示 %s 已存在，复用（退出时不会关闭它）", r.cfg.Display)
	} else {
		// 残留的 socket 会让 Xvfb 拒绝启动（Server is already active），先清掉
		if err := os.Remove(displaySocket(r.cfg.Display)); err != nil && !os.IsNotExist(err) {
			logger.Warnf("清理残留 X socket 失败: %v", err)
		}
		if err := r.startXvfb(ctx); err != nil {
			return err
		}
		r.ownsXvfb = true
		if err := r.waitForDisplay(ctx); err != nil {
			r.stopLocked()

			return err
		}
	}

	if err := r.startChrome(ctx); err != nil {
		r.stopLocked()

		return err
	}

	select {
	case <-ctx.Done():
		r.stopLocked()

		return ctx.Err()
	case <-time.After(r.cfg.Warmup):
	}

	r.started = true
	logger.Infof("虚拟屏已就绪: %s %dx%d，页面 %s", r.cfg.Display, r.cfg.Width, r.cfg.Height, r.cfg.URL)

	return nil
}

// Stop 收尾：关掉本次启动的 Chrome 与 Xvfb，删掉临时 profile。
func (r *Renderer) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopLocked()
}

func (r *Renderer) stopLocked() {
	if r.chrome != nil {
		killProcessGroup(r.chrome, "chrome")
		r.chrome = nil
	}
	if r.ownsXvfb && r.xvfb != nil {
		killProcessGroup(r.xvfb, "Xvfb")
		r.xvfb = nil
		r.ownsXvfb = false
	}
	if r.profileDir != "" {
		if err := os.RemoveAll(r.profileDir); err != nil {
			logger.Warnf("清理 chrome profile 失败: %v", err)
		}
		r.profileDir = ""
	}
	r.started = false
}

// startXvfb 起虚拟屏；子进程单独成组，便于整组收掉。
func (r *Renderer) startXvfb(ctx context.Context) error {
	args := []string{
		r.cfg.Display,
		"-screen", "0", fmt.Sprintf("%dx%dx%d", r.cfg.Width, r.cfg.Height, displayXDepth),
		"-nolisten", "tcp",
		"-noreset",
	}

	cmd := exec.CommandContext(ctx, r.cfg.Xvfb, args...)
	cmd.Stderr = &logWriter{name: "Xvfb"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("stream: 启动 Xvfb: %w", err)
	}
	r.xvfb = cmd
	// Xvfb 退出后回收，避免留僵尸进程
	go func() { _ = cmd.Wait() }()
	logger.Infof("已启动 Xvfb: %s %dx%d", r.cfg.Display, r.cfg.Width, r.cfg.Height)

	return nil
}

// waitForDisplay 等 X socket 出现；Xvfb 起得来但屏没就绪时抓屏会得到黑屏。
func (r *Renderer) waitForDisplay(ctx context.Context) error {
	socket := displaySocket(r.cfg.Display)
	deadline := time.Now().Add(displayReadyTimeout)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(displayPollInterval):
		}
	}

	return fmt.Errorf("stream: 等了 %s 还没看到 %s", displayReadyTimeout, socket)
}

// startChrome 全屏打开页面。参数照无人值守运行的需要配：
// 免登录态提示、免自动播放限制（否则不点一下就出不了声）。
func (r *Renderer) startChrome(ctx context.Context) error {
	profileDir, err := os.MkdirTemp("", "syagent-chrome-*")
	if err != nil {
		return fmt.Errorf("stream: 创建 chrome profile: %w", err)
	}
	r.profileDir = profileDir

	cmd := exec.CommandContext(ctx, r.cfg.Chrome, r.chromeArgs()...)
	cmd.Env = chromeEnv(r.cfg.Display)
	cmd.Stderr = &logWriter{name: "chrome"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("stream: 启动 Chrome: %w", err)
	}
	r.chrome = cmd
	go func() { _ = cmd.Wait() }()
	logger.Infof("已启动 Chrome（全屏），显示 %s", r.cfg.Display)

	return nil
}

// chromeEnv 给 Chrome 一份强制走 X11 的环境。
//
// 环境里的 WAYLAND_DISPLAY 会让 Chrome 优先连 Wayland：虚拟屏空转、抓屏抓到死屏。
// 实测症状是 Xvfb 起着、ffmpeg 却报 Cannot open display，Chrome 自己在日志里抱怨
// ozone-platform=wayland 与 Vulkan 不兼容。
func chromeEnv(display string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "WAYLAND_DISPLAY=") || strings.HasPrefix(item, "DISPLAY=") {
			continue
		}
		env = append(env, item)
	}

	return append(env, "DISPLAY="+display)
}

// chromeArgs 返回 Chrome 启动参数。
func (r *Renderer) chromeArgs() []string {
	args := []string{
		"--kiosk",
		// 本机是 Wayland 会话时必须显式走 X11，否则 Chrome 连的是 Wayland，虚拟屏上什么都没有
		"--ozone-platform=x11",
		// 虚拟屏上没有 GPU：不放开软件 WebGL，PIXI 起不来、画面就是空的
		// （Chrome 会报 WebGL1 blocklisted）
		"--enable-unsafe-swiftshader",
		"--use-gl=angle",
		"--use-angle=swiftshader",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-session-crashed-bubble",
		"--disable-translate",
		"--hide-scrollbars",
		// 无人值守时没有人能点页面，自动播放必须放开
		"--autoplay-policy=no-user-gesture-required",
		"--user-data-dir=" + r.profileDir,
		"--window-size=" + strconv.Itoa(r.cfg.Width) + "," + strconv.Itoa(r.cfg.Height),
		"--window-position=0,0",
	}
	// 以 root 跑（容器里常见）时 Chrome 默认拒绝启动
	if os.Geteuid() == 0 {
		args = append(args, "--no-sandbox")
	}

	return append(args, r.cfg.URL)
}

// displaySocket 把显示号映射到 X socket 路径：:99 → /tmp/.X11-unix/X99。
func displaySocket(display string) string {
	number := strings.TrimPrefix(display, ":")
	if host, _, found := strings.Cut(number, "."); found {
		number = host
	}

	return filepath.Join("/tmp/.X11-unix", "X"+number)
}

// displayAlive 真连一次 X socket 判断屏是否活着。
//
// 只 stat 文件不够：被 SIGKILL 掉的 Xvfb 会留下 socket 文件，判定成「已存在」就会
// 复用一块死屏，ffmpeg 报 Cannot open display（实测踩到过一次）。
func displayAlive(display string) bool {
	conn, err := net.DialTimeout("unix", displaySocket(display), displayAliveTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()

	return true
}

// killProcessGroup 先 SIGTERM 整组，等待宽限期后 SIGKILL。
func killProcessGroup(cmd *exec.Cmd, name string) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	deadline := time.Now().Add(killGrace)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pgid, 0); err != nil {
			return // 进程组已经没了
		}
		time.Sleep(displayPollInterval)
	}

	logger.Warnf("%s 没有在 %s 内退出，强制杀掉", name, killGrace)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}
