Type: grilling
Status: resolved

## Question

统一播报队列设计决策(PRD 四):优先级分层(SC > 礼物 > 普通弹幕 > LLM 主动互动 > 待机发言)与中断/排队语义;高优先级打断低优先级后的恢复规则;与 TTS 异步合成、前端音频帧序列(句序不乱、不重叠)的对接;`/inject` 播报与会话回复统一入队(消灭双播报管线);distillery 的冷却调度逻辑(llm-vup-bridge 并入)在 Go 侧如何表达。

## 进度：0%

下一步：grilling 用户确认优先级与打断语义。