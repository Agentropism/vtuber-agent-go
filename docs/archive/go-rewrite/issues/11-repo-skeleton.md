Type: grilling
Status: resolved

> **布局已被取代(2026-09-19)**:本票落地的顶层包结构(无 `internal/` 前缀)在根目录迁移中
> 改为 `cmd/` + `internal/`;module 路径为 `github.com/Agentropism/vtuber-agent-go`。
> 票面其余决议(无 asr 包、无 CGo、包边界)仍然成立。

## Question

onebot-gateway 仓库内演进的项目骨架决策:包布局(现有 cmd/onebot-gateway + internal/app|event|upload|filter|action|server|config 的保留/重构/删除);llm-vup-bridge distillery 逻辑并入位置(新包?);新增包:conversation、tts/、asr/、broadcast、frontend(静态托管)、bilibili Go WSS 客户端、memory;单二进制入口与模块边界(可单测);旧仓库代码搬迁规则(复制而非跨仓库引用)。

## 进度：100%(2026-09-16 骨架已落地)

## 决议更新(2026-09-16)

实际布局与票面草案有两处差异:**取消 `internal/` 前缀**(顶层目录即模块边界,与
AGENTS.md 的模块划分一致),**取消 `asr/` 包**(ASR 不做,见 ticket 03 决议更新)。

```text
app/                    装配
gateway/                接入层(只认平台协议)
  event/  bilibili/  server/  upload/  filter/  distillery/
agent/                  编排层(只认事件与会话)
  conversation/  conversation/llm/  broadcast/  memory/  frontend/  tool/
tts/                    云 TTS 引擎(5 家 + failover)
shared/                 跨模块契约(action 下行 Action;event 事件种类)
config/ logger/         基础设施
cmd/onebot-gateway/     入口
```

**无 CGo**:sherpa binding 不再引入,单二进制保持纯 Go。

仍是空壳(归属各自执行阶段):`gateway/bilibili/`(E2)、`agent/frontend/`(E6)、
`agent/memory/`、`agent/tool/`(E7)。

下一步：无(本票关闭)。