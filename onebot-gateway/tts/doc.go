// Package tts 提供云 TTS 引擎接口与实现(执行计划 E4)。
//
// 契约:引擎 = 「单句文本 → WAV PCM16 24kHz mono 字节」;failover 链按配置序降级。
// 保留引擎:edge_tts / openai_tts / siliconflow_tts / fish_api_tts / minimax_tts,
// 全部为远程 API;不做本地推理、不做音色克隆,云引擎返回 mp3 时在本包内转码为 PCM16。
package tts
