// Package conversation 提供会话编排核心(执行计划 E3)。
//
// 已实现:Agent —— 带对话历史的会话智能体,对应原 Python 实现的 BasicMemoryAgent。
// 它自己持有历史与系统提示词,每次请求把完整历史交给无状态的 llm 客户端;
// 模型要求调用工具时自动完成「执行工具 → 回灌结果 → 继续生成」的循环;
// 支持用户打断(改写最后一条助手消息并追加打断标记)。
//
// 待实现(仍属本包职责):Session 管理器(按 channel_id 一对一会话,互不阻塞)、
// 统一 InboundEvent 适配(平台上传 / /inject / proxy 三类输入)、历史裁剪、
// 角色提示词加载,以及为 memory 包留召回接口。
//
// 不移植的部分:MCP 提示词模式降级(计划 Q5:mcpp 重建为纯工具注册层,不引入
// 第二个循环)、句子分段与 TTS 预处理(归 E5 的 broadcast)。
package conversation
