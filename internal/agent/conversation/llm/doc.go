// Package llm 封装对 OpenAI 兼容远程端点的流式调用(执行计划 E3)。
//
// 契约:调用方给出一组消息、系统提示词与工具声明,本包负责发一次流式请求,
// 并把增量以 Event 回调出去;会话历史、角色提示词加载与工具执行都在调用方,
// 本包不持有任何对话状态。
//
// 实现基于 github.com/sashabaranov/go-openai,通过覆盖 base_url 适配
// DeepSeek 等 OpenAI 兼容服务(无本地推理)。
//
// 与原 Python 实现(Open-LLM-VTuber 的 agent/stateless_llm/openai_compatible_llm.py)
// 的两处有意差异:
//  1. 请求失败返回 error,不再把错误文案当作正文交给调用方——否则直播场景下
//     "Error calling the chat endpoint..." 会被当成 AI 回复送去 TTS 播出来。
//  2. 不实现"模型不支持工具就永久降级"的开关;工具支持与否由调用方每次请求决定。
package llm
