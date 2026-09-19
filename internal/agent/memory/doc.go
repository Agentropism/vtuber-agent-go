// Package memory 提供会话的长期记忆（执行计划 E7）。
//
// 实现见 memory.go：追加式 JSON Lines 存储 + 关键词召回（无向量库）。
// SC/礼物写入带权重标记，召回打分按 命中词元数 × 权重 ÷ 时间衰减；
// 范围以支撑会话连续性为度，不做复杂 RAG。
//
// 决策票 08 原定用 SQLite，2026-09-17 修订为追加式文件，理由见 memory.go 包注释。
package memory
