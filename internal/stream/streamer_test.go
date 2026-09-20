package stream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArgsForScreen(t *testing.T) {
	streamer, err := New(Config{
		Output:  "rtmp://example.com/live/stream?key=secret",
		Display: ":99",
	})
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	defer streamer.Close()

	args := strings.Join(streamer.Args(), " ")

	for _, want := range []string{"-f x11grab", "-i :99", "-video_size 1280x720", "-framerate 30", "-f s16le", "-c:v libx264", "-pix_fmt yuv420p", "-c:a aac", "-f flv"} {
		if !strings.Contains(args, want) {
			t.Fatalf("参数里缺少 %q：\n%s", want, args)
		}
	}
	// 音频管道必须指向真实存在的 FIFO，否则 ffmpeg 起不来
	if !strings.Contains(args, streamer.audioPath) {
		t.Fatalf("音频输入没有指向管道 %s：\n%s", streamer.audioPath, args)
	}
	if info, err := os.Stat(streamer.audioPath); err != nil {
		t.Fatalf("音频管道不存在: %v", err)
	} else if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("音频管道不是命名管道")
	}
	if !strings.Contains(args, "rtmp://example.com") {
		t.Fatalf("推流地址没有传给 ffmpeg：\n%s", args)
	}
}

func TestArgsForTestInput(t *testing.T) {
	streamer, err := New(Config{Output: "/tmp/out.flv", Input: InputTest, Width: 640, Height: 360, FPS: 15})
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	defer streamer.Close()

	args := strings.Join(streamer.Args(), " ")
	if !strings.Contains(args, "testsrc=size=640x360:rate=15") {
		t.Fatalf("测试画面参数不对：\n%s", args)
	}
	if strings.Contains(args, "x11grab") {
		t.Fatalf("测试模式不该抓屏：\n%s", args)
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("没有推流地址应当报错")
	}
	if _, err := New(Config{Output: "rtmp://x", Input: "nope"}); err == nil {
		t.Fatal("未知来源应当报错")
	}
	if _, err := New(Config{Output: "rtmp://x", FFmpeg: "definitely-not-a-binary"}); err == nil {
		t.Fatal("找不到 ffmpeg 应当报错")
	}
}

// 密钥不能出现在日志或错误信息里。
func TestClipURL(t *testing.T) {
	got := clipURL("rtmp://push.example.com/live-bvc/?streamname=abc&key=deadbeef")
	if strings.Contains(got, "deadbeef") {
		t.Fatalf("密钥没有隐去: %s", got)
	}
	if !strings.HasPrefix(got, "rtmp://push.example.com") {
		t.Fatalf("地址被改坏了: %s", got)
	}
	if got := clipURL("rtmp://push.example.com/live/abc"); got != "rtmp://push.example.com/live/abc" {
		t.Fatalf("没有密钥时不该改动: %s", got)
	}
}

// 真跑一次 ffmpeg：测试画面 + 管道音频 → 本地 flv，再用 ffprobe 核对音视频轨都在。
func TestRunProducesVideoAndAudio(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("环境里没有 ffmpeg")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("环境里没有 ffprobe")
	}

	out := filepath.Join(t.TempDir(), "out.flv")
	streamer, err := New(Config{
		Output:       out,
		Input:        InputTest,
		FFmpeg:       ffmpeg,
		Width:        320,
		Height:       240,
		FPS:          15,
		VideoBitrate: "300k",
		RestartWait:  -1, // 退出就结束，不要重试
	})
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	defer streamer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- streamer.Run(ctx) }()

	// 往管道里灌一秒的 PCM，验证音频轨真的有数据
	go func() {
		pcm := make([]byte, 24000*2) // 1 秒 24kHz 单声道 16bit
		for i := range pcm {
			pcm[i] = byte(i % 251)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			streamer.WritePCM(pcm)
			time.Sleep(200 * time.Millisecond)
		}
	}()

	// 等输出攒够字节再收工：固定 sleep 在并行跑测试（别的用例在起 Xvfb/Chrome）时会 flake
	waitDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(waitDeadline) {
		if info, err := os.Stat(out); err == nil && info.Size() > 10_000 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	<-done

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("没有生成输出文件: %v", err)
	}
	if info.Size() < 10_000 {
		t.Fatalf("输出文件太小（%d 字节），推流可能没真的跑起来", info.Size())
	}

	probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type,codec_name",
		"-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	text := string(probe)
	if !strings.Contains(text, "h264") {
		t.Fatalf("输出里没有 h264 视频轨: %s", text)
	}
	if !strings.Contains(text, "aac") {
		t.Fatalf("输出里没有 aac 音频轨: %s", text)
	}
	t.Logf("输出 %d 字节，轨道：%s", info.Size(), strings.TrimSpace(text))
}
