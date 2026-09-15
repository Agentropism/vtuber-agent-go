# TTS 侧 Go 库调研（执行计划 E4）

> 调研日期 2026-09-15。结论均基于**实测**：候选库拉到本地读源码 + 实际调用 + ffprobe 校验输出。
> 前置事实来自 `02-tts-asr-engine-survey.md`（5 家保留引擎的协议形态）。

## 1. 结论速览

| 引擎 | 协议复杂度 | Go 方案 | 新增依赖 |
|---|---|---|---|
| `edge_tts` | **高**（逆向 WSS + `Sec-MS-GEC` 签名 + 二进制帧） | 用 `github.com/wujunwei928/edge-tts-go` | gorilla/websocket、go-resty/resty、google/uuid |
| `openai_tts` | 低 | **已有依赖** `go-openai` 的 `CreateSpeech` | **无** |
| `siliconflow_tts` | 低（OpenAI 兼容） | 手写 `net/http` | 无 |
| `fish_api_tts` | 低 | 手写 `net/http`（官方 SDK 可选） | 无 |
| `minimax_tts` | 中（SSE + hex 音频） | 手写 `net/http` + `encoding/hex` | 无 |
| mp3 解码（仅 edge 需要） | — | `github.com/hajimehoshi/go-mp3` | 1 个，纯 Go 无 cgo，**实际不进构建图**（见 §4） |

**一句话**：只有 `edge_tts` 值得引入第三方库，其余四家手写更划算（合计约 200~250 行）。

## 2. edge-tts-go 实测

`github.com/wujunwei928/edge-tts-go` v0.0.2（2026-06-13）。库代码在 `edge_tts/` 子包，仓库根是 CLI（cobra）。

API：

```go
c, err := edge.NewCommunicate(text,
    edge.SetVoice("zh-CN-XiaoxiaoNeural"),
    edge.SetRate("+0%"), edge.SetVolume("+0%"), edge.SetPitch("+0Hz"),
    edge.SetReceiveTimeout(20),           // 秒
)
audio, err := c.Stream()                   // 返回完整 mp3 字节（内部缓冲，非真流式）
```

实测结果：

| 项 | 值 |
|---|---|
| 成功率 | 20/20 |
| 音频大小 | 14688 字节（"你好，这是一个测试。"） |
| 耗时 | 482 ~ 846 ms |
| ffprobe 校验 | `format_name=mp3, duration=2.448s, bit_rate=48000` |
| 解码后采样率 | **24000 Hz** |

声明的输出格式是 `audio-24khz-48kbitrate-mono-mp3`，与 ffprobe 结果一致。**采样率正好等于目标格式的 24kHz，所以 edge 只需要解码、不需要重采样。**

### 2.1 坑一：`Stream()` 不支持 ctx 取消

`context` 在该库中只用于拨号超时：

```go
dialCtx, dialContextCancel := context.WithTimeout(context.Background(), time.Duration(c.receiveTimeout)*time.Second)
```

连接建立之后的读取循环没有任何取消机制，只能等 `receiveTimeout`。要支持打断（barge-in）必须自己包一层 goroutine + `ctx.Done()`，或改为自研协议实现。

### 2.2 坑二：`Stream()` 内的窄窗口竞态（读码所得，未复现）

```go
go func() {
    defer func() {
        close(finished)
        close(failed)      // 与上一行之间构成窗口
    }()
    ...
}()
...
select {
case <-finished:            // 返回 audioData
case errMessage := <-failed: // 返回 nil, nil
}
```

两个 channel 都被关闭后，`select` 在两个就绪分支间随机选择。若主协程恰在两次 `close` 之间被唤醒，会命中 `failed` 分支返回 `(nil, nil)`。实测 20/20 正常，说明窗口极窄。

**防御方式**：把「空音频 + 无错误」当作失败交给 failover 链，不要把空字节当成功结果送进 TTS 任务。

### 2.3 其余观察

- 支持的输出格式只有 `mp3` / `mp3-hq` / `webm-opus`，**没有 raw PCM/WAV**。
- `SetOutputFormat` 里的 `_ = edgeFormat` 看似 bug，实际不影响：构建请求时（`communicate.go:393`）会重新做映射。
- `go-resty/resty` 只被 `list_voices.go` 使用，但同包编译，仍会进入构建图。
- 仓库内该核心路径**没有测试**（只有 `output_format_test.go`）。

## 3. 各引擎输出格式（决定是否需要转码）

| 引擎 | 可要求格式 | 采样率 | 转码需求 |
|---|---|---|---|
| edge | 库里只有 mp3/mp3-hq/webm-opus | **24kHz mono** | 仅 mp3 解码 + 下混单声道 |
| openai | `response_format` 支持 pcm/wav/flac（`go-openai` 已含全部常量） | pcm = 24kHz mono | **无** |
| siliconflow | `response_format` + `sample_rate` 可配 | 可指定 24000 | **无** |
| fish | `format`: mp3/wav/pcm/opus（官方 SDK 常量齐备） | — | **无** |
| minimax | `format`（Python 侧用 mp3），示例 `sample_rate: 32000, bitrate: 128000` | 32kHz | 解码 + **重采样 32k→24k** |

→ 4/5 可以直接索要 PCM/WAV；只有 edge 必须解码，且它的采样率天然匹配。

## 4. `hajimehoshi/go-mp3` 实测

v0.3.4（2022-11-02，稳定无变化）。生产代码只有 `decode.go` + `source.go`，仅用标准库。

- **实际零依赖**：其 `go.mod` 虽 `require github.com/hajimehoshi/oto/v2`，但 oto 只被 example/其他包使用。探针模块 `go mod tidy` 后 go.mod 里没有 oto，`go list -deps` 中 oto 计数为 0。
- **重要陷阱**：源码 `decode.go:201` 原文 —— *"The stream is always formatted as 16bit (little endian) 2 channels"*。**它永远输出双声道**，即使源是单声道。

实测印证：edge 的单声道 mp3 解出 235008 字节，而按 2.448s × 24000Hz × 2 字节应为 117504 字节 —— 正好 2 倍。直接用会导致时长翻倍且不符合「PCM16 24kHz mono」契约，**必须自行下混为单声道**。

WAV 头（44 字节）自己写即可，不需要引入 wav 库。

## 5. 建议实现路径

1. 建 `tts/` 包，引擎接口按计划 E4：**「单句文本 → WAV PCM16 24kHz mono 字节」**；failover 按配置顺序降级。
2. `edge_tts` 用现成库 —— `Sec-MS-GEC` 是跟随 Edge 版本漂移的反向协议（库内硬编码 `CHROMIUM_FULL_VERSION`），自维护成本高于 3 个依赖的成本。用接口把它包住，将来要换/要 fork 只影响一个文件。
3. 其余四家手写 `net/http`；`openai_tts` 可直接复用已有依赖 `go-openai` 的 `CreateSpeech`（`SpeechResponseFormatPcm` 常量已具备），`siliconflow_tts` 因需要 `sample_rate`/`gain` 而 go-openai 请求结构未覆盖，改手写。
4. mp3 解码只加 `go-mp3`，并**统一在解码后下混单声道**。
5. 「空音频 + 无错误」纳入失败判定，交给 failover。

## 6. 待确认

- minimax 是否支持直接返回 PCM 或可配 24kHz（若支持可省掉重采样）。
- fish 用官方 SDK（`fishaudio/fish-audio-go` v1.1.0，会带进 gorilla/websocket + msgpack，因同包含 WS 流式实现）还是手写一次性 `Convert`（普通 POST，无新依赖）。

## 7. 附：用于验证的最小复现

`/tmp/ttsprobe` 下的临时模块曾用于实测（不在仓库内）：调用 edge 库合成、写盘、ffprobe 校验、go-mp3 解码并打印采样率与 PCM 字节数。
