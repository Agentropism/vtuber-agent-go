// Package conversation 提供会话编排核心(执行计划 E3)。
//
// 职责:Session 管理器(按 channel_id 一对一会话,互不阻塞)、统一 InboundEvent
// 适配(平台上传 / /inject / proxy 三类输入)、历史裁剪、角色提示词加载、
// OpenAI 兼容 LLM 流式调用。为 memory 包留召回接口。
package conversation