// Package broadcast 提供统一播报队列(执行计划 E5)。
//
// 职责:优先级分层(SC > 礼物 > 弹幕 > LLM 主动 > 待机)、高优先级打断与低优先级重排队、
// 冷却调度、TTS 任务并行合成与按序投递;`/inject` 播报与会话回复统一入队,消灭双播报管线。
package broadcast
