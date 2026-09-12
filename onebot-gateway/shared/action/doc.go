// Package action 定义下行 Action 契约(OneBot 11)。
//
// 放在 shared 的原因:gateway 负责把 Action 写回平台客户端,agent 负责决定发什么,
// 两侧都要用到这个类型,而 gateway 依赖 agent、不能反向依赖。本包零内部依赖,
// 只包含协议类型与构造函数,不含网络与状态。
package action
