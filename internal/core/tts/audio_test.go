package tts

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestDecodeMP3DownmixesToMono 是回归测试：go-mp3 永远输出 16bit 双声道，
// 忘记下混时这里的字节数会翻倍，时长也随之翻倍。
func TestDecodeMP3DownmixesToMono(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "tone-24k-mono.mp3"))
	if err != nil {
		t.Fatalf("读取测试夹具失败: %v", err)
	}

	pcm, rate, err := DecodeMP3(data)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if rate != SampleRate {
		t.Errorf("采样率不符合预期: %d", rate)
	}

	// 夹具是 0.3 秒、24kHz、单声道 → 7200 个采样点 = 14400 字节。
	// mp3 编解码会引入约 1100 个采样点的延迟与帧对齐填充，所以实际略多；
	// 若忘记下混，字节数会直接翻到 28800 量级。
	want := SampleRate * BytesPerSample * 3 / 10
	if len(pcm) < want {
		t.Fatalf("解码字节数 %d 少于音频本身的 %d", len(pcm), want)
	}
	if len(pcm) >= want*2 {
		t.Fatalf("解码字节数 %d 达到双声道量级（%d），说明忘记下混", len(pcm), want*2)
	}
}

func TestDecodeMP3RejectsBadInput(t *testing.T) {
	if _, _, err := DecodeMP3(nil); err == nil {
		t.Error("空数据应报错")
	}
	if _, _, err := DecodeMP3([]byte("这不是 mp3 数据")); err == nil {
		t.Error("非法数据应报错")
	}
}

func TestDownmixStereoToMono(t *testing.T) {
	// 两帧：(100, 200) 与 (-100, -300)，下混后应为 150 与 -200
	values := []int16{100, 200, -100, -300}
	stereo := make([]byte, len(values)*2)
	for i, v := range values {
		binary.LittleEndian.PutUint16(stereo[i*2:], uint16(v))
	}

	mono := DownmixStereoToMono(stereo)
	if len(mono) != 4 {
		t.Fatalf("下混后长度不符合预期: %d", len(mono))
	}
	if got := int16(binary.LittleEndian.Uint16(mono[0:])); got != 150 {
		t.Errorf("第一帧平均不符合预期: %d", got)
	}
	if got := int16(binary.LittleEndian.Uint16(mono[2:])); got != -200 {
		t.Errorf("第二帧平均不符合预期: %d", got)
	}
}

func TestResampleHalvesSampleCount(t *testing.T) {
	const inSamples = 480
	pcm := make([]byte, inSamples*2)
	for i := 0; i < inSamples; i++ {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(i)))
	}

	out := Resample(pcm, 48000, 24000)
	if len(out)/2 != inSamples/2 {
		t.Fatalf("重采样后点数不符合预期: %d", len(out)/2)
	}
	if got := int16(binary.LittleEndian.Uint16(out[0:])); got != 0 {
		t.Errorf("首点不符合预期: %d", got)
	}
	// 线性插值应保持斜坡单调递增
	for i := 1; i < len(out)/2; i++ {
		prev := int16(binary.LittleEndian.Uint16(out[(i-1)*2:]))
		curr := int16(binary.LittleEndian.Uint16(out[i*2:]))
		if curr < prev {
			t.Fatalf("第 %d 点出现回退: %d → %d", i, prev, curr)
		}
	}
}

func TestResampleAndEnsureContractNoOpAtContractRate(t *testing.T) {
	pcm := []byte{1, 0, 2, 0}

	if got := Resample(pcm, SampleRate, SampleRate); len(got) != len(pcm) {
		t.Errorf("采样率相同时不应改变数据: %d", len(got))
	}
	if got := EnsureContract(pcm, SampleRate); len(got) != len(pcm) {
		t.Errorf("EnsureContract 在契约采样率下应原样返回: %d", len(got))
	}
	if got := EnsureContract(pcm, 32000); len(got) == 0 {
		t.Error("非契约采样率应重采样而非返回空")
	}
}
