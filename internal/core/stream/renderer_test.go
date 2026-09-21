package stream

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDisplaySocket(t *testing.T) {
	cases := map[string]string{
		":99":   "/tmp/.X11-unix/X99",
		":0":    "/tmp/.X11-unix/X0",
		":99.0": "/tmp/.X11-unix/X99",
	}
	for display, want := range cases {
		if got := displaySocket(display); got != want {
			t.Fatalf("displaySocket(%q) = %q, want %q", display, got, want)
		}
	}
}

// 残留的 socket 文件不能被当成活着的屏：被 SIGKILL 的 Xvfb 会留下它，
// 复用一块死屏的表现是 ffmpeg 报 Cannot open display（实测踩到过）。
func TestDisplayAliveRejectsStaleSocket(t *testing.T) {
	const display = ":76"
	socket := displaySocket(display)
	if _, err := os.Stat(socket); err == nil {
		t.Skipf("%s 已存在，跳过以免误伤", socket)
	}

	// 普通文件冒充 socket：文件在，但连不上
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Skipf("无权在 /tmp/.X11-unix 下造文件: %v", err)
	}
	defer func() { _ = os.Remove(socket) }()

	if displayAlive(display) {
		t.Fatal("残留文件不该被判定为活着的显示")
	}
}
func TestNewRendererRejectsBadConfig(t *testing.T) {
	if _, err := NewRenderer(RendererConfig{}); err == nil {
		t.Fatal("没有页面地址应当报错")
	}
	if _, err := NewRenderer(RendererConfig{URL: "about:blank", Xvfb: "definitely-not-a-binary"}); err == nil {
		t.Fatal("找不到 Xvfb 应当报错")
	}
	if _, err := NewRenderer(RendererConfig{URL: "about:blank", Chrome: "definitely-not-a-binary"}); err == nil {
		t.Fatal("找不到 Chrome 应当报错")
	}
}

// Chrome 必须免点击就能出声，否则无人值守推流只能推静音画面。
func TestChromeArgsForUnattendedRun(t *testing.T) {
	renderer, err := NewRenderer(RendererConfig{URL: "http://127.0.0.1:6199/?autostart=1", Width: 1280, Height: 720})
	if err != nil {
		t.Skipf("环境里没有 Xvfb/Chrome: %v", err)
	}

	renderer.profileDir = "/tmp/profile"
	args := strings.Join(renderer.chromeArgs(), " ")

	for _, want := range []string{
		"--kiosk",
		"--autoplay-policy=no-user-gesture-required",
		"--window-size=1280,720",
		"--user-data-dir=/tmp/profile",
		"autostart=1",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("Chrome 参数里缺少 %q：\n%s", want, args)
		}
	}
	if !strings.HasSuffix(args, "http://127.0.0.1:6199/?autostart=1") {
		t.Fatalf("页面地址必须是最后一个参数：\n%s", args)
	}
}

// 真起一次虚拟屏：X socket 出现才算就绪，Stop 之后应连屏一起收掉。
func TestRendererStartsAndStopsDisplay(t *testing.T) {
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("环境里没有 Xvfb")
	}
	if _, err := exec.LookPath("google-chrome-stable"); err != nil {
		t.Skip("环境里没有 google-chrome-stable")
	}

	const display = ":77"
	if displayAlive(display) {
		t.Skipf("显示 %s 已被占用，跳过以免误伤", display)
	}

	renderer, err := NewRenderer(RendererConfig{
		Display: display,
		URL:     "about:blank",
		Width:   320,
		Height:  240,
		Warmup:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("构造渲染器: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := renderer.Start(ctx); err != nil {
		t.Fatalf("启动渲染器: %v", err)
	}
	if !displayAlive(display) {
		t.Fatal("启动后应当能连上 X 显示")
	}

	renderer.Stop()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !displayAlive(display) {
			return // 屏已经收掉
		}
		time.Sleep(100 * time.Millisecond)
	}

	_ = os.Remove(displaySocket(display))
	t.Fatal("Stop 之后 X socket 仍然存在")
}
