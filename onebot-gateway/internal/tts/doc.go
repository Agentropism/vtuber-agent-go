// Package tts 提供 TTS 引擎接口与实现(执行计划 E4)。
//
// 契约:引擎 = 「单句文本 → WAV PCM16 24kHz mono 字节」;failover 链按配置序降级;
// 保留引擎:edge_tts / openai_tts / siliconflow_tts / fish_api_tts / minimax_tts
// 以及 sherpa-onnx 官方 Go binding(VITS,复用 models/ 权重)。
package tts