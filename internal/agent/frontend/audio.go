package frontend

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"time"

	"github.com/Agentropism/vtuber-agent-go/internal/tts"
)

// wavHeaderSize 是标准 PCM WAV 头长度。
const wavHeaderSize = 44

// encodeWAV 给裸 PCM 补一个标准 WAV 头（RIFF/WAVE/fmt/data）。
//
// 浏览器只认容器格式：<audio> 与解码器都不吃裸 PCM，而 tts 包的契约恰好是裸 PCM，
// 所以在最靠近浏览器的一层补头，避免让每个 TTS 引擎各自拼容器。
func encodeWAV(pcm []byte) []byte {
	out := make([]byte, wavHeaderSize+len(pcm))

	byteRate := tts.SampleRate * tts.Channels * tts.BytesPerSample
	blockAlign := tts.Channels * tts.BytesPerSample

	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+len(pcm)))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16) // fmt 块长度
	binary.LittleEndian.PutUint16(out[20:22], 1)  // 编码：1 = PCM
	binary.LittleEndian.PutUint16(out[22:24], uint16(tts.Channels))
	binary.LittleEndian.PutUint32(out[24:28], uint32(tts.SampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(out[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(out[34:36], uint16(tts.BytesPerSample*8))
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(pcm)))
	copy(out[wavHeaderSize:], pcm)

	return out
}

// encodeAudio 把裸 PCM 打包成浏览器可直接播放的 base64 WAV。
func encodeAudio(pcm []byte) string {
	var buf bytes.Buffer
	buf.Grow(base64.StdEncoding.EncodedLen(wavHeaderSize + len(pcm)))
	encoder := base64.NewEncoder(base64.StdEncoding, &buf)
	_, _ = encoder.Write(encodeWAV(pcm))
	_ = encoder.Close()

	return buf.String()
}

// audioDuration 返回一段 PCM 的播放时长。
func audioDuration(pcm []byte) time.Duration {
	if len(pcm) == 0 {
		return 0
	}

	frames := len(pcm) / (tts.Channels * tts.BytesPerSample)
	seconds := float64(frames) / float64(tts.SampleRate)

	return time.Duration(seconds * float64(time.Second))
}
