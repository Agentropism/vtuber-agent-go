// Package tool 提供 Tool 注册层(执行计划 E7,mcpp 重建的 Go 落点)。
//
// 职责:注册「名称 + 参数描述 + 执行函数」,供 conversation 在 LLM 工具调用时查询与执行;
// 首批工具:状态查询、记忆检索。只做注册与调度,不引入第二个 Agent 循环,
// 也不做 MCP 协议兼容层。
package tool
