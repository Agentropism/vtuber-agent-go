// Package bilibili 提供 B 站开放平台 WSS 客户端(执行计划 E2/E12)。
//
// 职责:开放平台长链连接、签名与心跳、断线重连(连续 5 次失败告警退出)、
// 11 类事件线格式解码与宽容化反序列化、未知/异常事件透传不丢弃。
// 等价 bilibili-live(Rust)能力,bilibili-live 仓库自 2026-09 冻结。
package bilibili