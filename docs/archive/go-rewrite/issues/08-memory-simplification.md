Type: grilling
Status: resolved

> **PRD 引用失效(2026-09-19)**:本票讨论的「PRD 九第一块(SC 高权重)」所对应的 `docs/PRD.md`
> 已随旧三子系统架构一并删除;实际落地的记忆方案为追加式 JSON Lines + 关键词二元组召回
> (见 `docs/MEMORY_API.md`),SC/礼物权重在写入时标记并参与召回打分。

## Question

记忆简化方案决策:废弃 memory.py(SQLite 关键词召回),Go 版方案——聊天历史持久化 + 轻量召回(关键词 SQLite / 简单向量嵌入 API?);是否纳入 SC 高权重(PRD 九第一块:SC > 弹幕 > 通知的写入/召回权重);范围以「支撑会话连续性与基础人设记忆」为度,不做复杂 RAG。

## 进度：0%

下一步：grilling 用户确认记忆范围与权重。