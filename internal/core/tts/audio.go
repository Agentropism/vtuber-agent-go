package tts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/Agentropism/vtuber-agent-go/internal/core/logger"
	"github.com/hajimehoshi/go-mp3"
)

// DecodeMP3 把 mp3 字节解码为单声道 PCM16，并返回源采样率。
//
// go-mp3 永远输出 16bit 双声道（其 decode.go 明示），所以这里必须下混：
// 否则单声道源的时长会翻倍，也不符合本包契约。调用方拿到返回值后应再用
// EnsureContract 把采样率对齐到 SampleRate。
func DecodeMP3(data []byte) ([]byte, int, error) {
	if len(data) == 0 {
		return nil, 0, errors.New("mp3 数据为空")
	}

	decoder, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("初始化 mp3 解码器: %w", err)
	}

	raw, err := io.ReadAll(decoder)
	if err != nil {
		return nil, 0, fmt.Errorf("解码 mp3: %w", err)
	}
	if len(raw) == 0 {
		return nil, 0, errors.New("mp3 解码结果为空")
	}

	return DownmixStereoToMono(raw), decoder.SampleRate(), nil
}

// DownmixStereoToMono 把 16bit 双声道交错 PCM 下混为单声道（左右取平均）。
func DownmixStereoToMono(pcm []byte) []byte {
	frames := len(pcm) / 4
	out := make([]byte, 0, frames*2)

	for i := 0; i < frames; i++ {
		left := int32(int16(binary.LittleEndian.Uint16(pcm[i*4:])))
		right := int32(int16(binary.LittleEndian.Uint16(pcm[i*4+2:])))
		out = binary.LittleEndian.AppendUint16(out, uint16(int16((left+right)/2)))
	}

	return out
}

// Resample 把单声道 PCM16 从 from 采样率线性插值到 to 采样率。
//
// 仅作为引擎原生采样率与契约不一致时的兜底；应优先让引擎直接返回 24kHz。
// 线性插值在降采样时会有混叠，语音场景可接受，不适合音乐。
func Resample(pcm []byte, from, to int) []byte {
	inSamples := len(pcm) / 2
	if from <= 0 || to <= 0 || from == to || inSamples < 2 {
		return pcm
	}

	outSamples := int(int64(inSamples) * int64(to) / int64(from))
	if outSamples <= 0 {
		return nil
	}

	out := make([]byte, 0, outSamples*2)
	ratio := float64(from) / float64(to)

	for i := 0; i < outSamples; i++ {
		position := float64(i) * ratio
		index := int(position)
		frac := position - float64(index)

		if index >= inSamples-1 {
			index = inSamples - 2
			frac = 1
		}
		if index < 0 {
			index = 0
			frac = 0
		}

		left := float64(int16(binary.LittleEndian.Uint16(pcm[index*2:])))
		right := float64(int16(binary.LittleEndian.Uint16(pcm[(index+1)*2:])))
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(left+(right-left)*frac)))
	}

	return out
}

// EnsureContract 把单声道 PCM16 对齐到契约采样率；已经是 24kHz 时原样返回。
func EnsureContract(pcm []byte, sampleRate int) []byte {
	if sampleRate == SampleRate || sampleRate <= 0 {
		return pcm
	}

	logger.Warnf("TTS 采样率 %d Hz 与契约 %d Hz 不一致，已重采样", sampleRate, SampleRate)
	return Resample(pcm, sampleRate, SampleRate)
}
