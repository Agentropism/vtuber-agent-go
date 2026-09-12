// Package asr 提供 ASR 引擎接口与实现(执行计划 E4)。
//
// 契约:音频字节 → 文本;保留引擎:groq_whisper_asr / azure_asr(远程),
// sherpa-onnx Go binding(SenseVoice,复用 models/ 权重,覆盖中文)。
package asr