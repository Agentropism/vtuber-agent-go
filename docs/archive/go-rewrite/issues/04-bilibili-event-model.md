Type: grilling
Status: resolved

## Question

Go 版 B 站事件模型设计决策:直接复用 onebot-gateway 现有 internal/event/bilibililive 包(11 类 cmd、宽容化反序列化、Unknown 透传)还是重设计?要点:OpenPlatformPacket struct 是否照搬;open_id 优先/uid 兜底身份规则是否维持;事件 → platformEvent 上传映射是否维持(含礼物/SC/大航海文本化格式);未知/异常事件透传策略。

## 进度：0%

下一步：grilling 用户确认事件模型方案。