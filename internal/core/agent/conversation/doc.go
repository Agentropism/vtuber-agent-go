// Package conversation 提供会话编排核心（执行计划 E3）。
//
// 组成：
//   - Agent（agent.go）：带对话历史的会话智能体，对应原 Python 的 BasicMemoryAgent。
//     自己持有历史与系统提示词，每次请求把完整历史交给无状态的 llm 客户端；
//     模型要求工具时自动完成「执行工具 → 回灌结果 → 继续生成」的循环；支持打断
//     （改写最后一条助手消息并追加打断标记）；历史按轮数与 token 上限裁剪（history.go）。
//   - Sessions（session.go）：按 channel_id 一对一会话，同渠道串行、跨渠道并行；
//     统一 InboundEvent 适配（平台上传 / 内部注入），出口是 ReplyFunc 文本下行与
//     Broadcaster 播报入队。
//   - Splitter（sentence.go）：流式回复按句切分，逐句入播报队列以降低首字延迟。
//   - 主动发言（idle.go）：渠道静默够久时主动说一句，走最低优先级。
//
// 长期记忆通过 Memory 接口注入（接口归调用方所有，实现在 agent/memory）。
// 角色提示词与表情词表由调用方在装配时给出，本包不做文件读取。
//
// 不移植的部分：MCP 提示词模式降级（计划 Q5：mcpp 重建为纯工具注册层，不引入第二个循环）。
package conversation
