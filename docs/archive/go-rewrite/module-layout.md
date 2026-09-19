# Go 重写:模块划分(定稿 · 已落地)

> 关联:`map.md`(基线)、`execution-plan.md`(E0–E8)。
> 状态:**目录重构已完成**(2026-09 本轮),`go build/vet/test` 全绿;后续按 E1–E7 填实现。

> **已被取代(2026-09-19)**:仓库根已上提为 Go 模块根,包全部移入 `internal/`,入口改为
> `cmd/vtuber-agent-go`,module 路径改为 `github.com/Agentropism/vtuber-agent-go`。
> 本文第 1 节的目录树、第 2 节「取消 `internal/` 前缀」的决议、以及第 4 节「本轮已完成」
> 中的路径均已失效;现状见根 `AGENTS.md`,迁移决议见根 `GRILL-ME.md`。

## 1. 模块结构(3 模块 + shared)

```text
app/                 装配:config → logger → 注入 → 注册 handler → 路由
gateway/             接入层:只认平台协议,不认会话/LLM
  event/               平台分发 event.Dispatch + onebot/ + bilibililive/(纯数据)
  bilibili/            B站开放平台 WSS 客户端(待实现,只放连接不放事件字段)
  server/              WS 接入与 Action 写回 + POST /inject 播报注入
  upload/              上行管线(暂存区/队列/进程内终端处理器)
  filter/              去重 + 敏感词
agent/               编排层:只认事件与会话,不认平台协议
  conversation/        会话/LLM 编排(已实现:会话管理 + 单会话 Agent)
  broadcast/           统一播报队列:优先级/打断/冷却/TTS 任务调度(已实现)
  memory/              SQLite 历史 + SC 权重 + 关键词召回(待实现)
  frontend/            /client-ws + 静态资源托管(待实现)
  tool/                Tool 注册层(待实现)
tts/                 云 TTS 引擎(待实现)
shared/              跨模块契约(零内部依赖)
  action/              下行 Action 契约(gateway 与 agent 都要用)
config/ logger/      基础设施
cmd/onebot-gateway/  入口
web/                 前端静态资产(E6 创建,不进编译)
```

依赖方向:**`app → {gateway, agent, tts} → shared`**,`gateway → agent` 单向。反向一律用函数注入破环(先例 `gateway/upload.SetActionForwarder`),不引入 DI 容器。

## 2. 相对决策 11 的修订

| 项 | 原决议 | 现决议 | 理由 |
| --- | --- | --- | --- |
| ASR | 保留云 ASR 2 + sherpa SenseVoice | **整包删除** | 前端无麦克风采集(`frontend/` 无 getUserMedia),`asr_model` 无输入源;VAD 仅为 ASR 切句服务,一并剪除 |
| TTS | 云 5 + sherpa VITS(CGo) | **仅云 5 家** | 去掉 CGo 工具链与 `models/` 1.5G 权重,`GOOS=linux go build` 一把过、"单二进制"才成立;断网/配额靠 failover 链 |
| `internal/` 前缀 | 决策 11 列 16 包 | **全部改顶层目录** | 目录树即模块边界;将来拆 `go.mod` 是纯机械移动 |
| 模块数 | — | **3 + shared** | asr 删除后只剩一个能力模块 |

## 3. 边界规则(实现时遵守)

1. 依赖只向下;`tts`/`asr` 类比的能力模块不得反向 import `agent`。**接口归调用方**:agent 定义 `Synthesizer`,tts 只提供实现。
2. `gateway/event/bilibililive` = **只放数据**(cmd 常量、事件结构、宽容化 JSON);`gateway/bilibili` = **只放连接**(鉴权/签名/心跳/重连)。前者不建连接,后者不定义字段。
3. 新模块接入不改分发核心,只挂既有扩展点:
   - `app.Register()` 里 `XxxActions.Add(handler)` 注册事件处理;
   - `server.ProvideServer` 的 mux 挂新路由(`/inject`、`/client-ws`、`/live2d-models/*`);
   - `SetLogger` / `SetXxxFunc` 注入,`Init()`/`Shutdown()` 成对管理生命周期。
4. 单文件 ≤ 400 行;超限按职责拆文件,**不拆包**(先例:`gateway/upload/client.go` 633 行 → 拆 `client.go`/`queue.go`/`event.go`,包名与 API 不变)。
5. `upload` 的 WS 出口**保留到切换完成**:它就是回滚开关(可指向旧 Python 服务并行观察)。

## 4. 本轮已完成

- `git mv` 全部既有包到新树(历史保留),import 路径批量重写,**未改任何逻辑**;
- 删除 `internal/asr`;`tts/doc.go` 改述为云引擎;
- 新增 `agent/tool/doc.go`、`shared/action/doc.go`;
- `AGENTS.md` 补"模块划分"节;`docs/UPLOAD_API.md` 路径同步;
- 验证:`go build ./... && go vet ./... && go test ./...` 全绿(6 个测试包)。

遗留(未处理,非本轮范围):`gateway/event/bilibililive/message.go` 存在**既有的** gofmt 字段对齐漂移(结构体 tag 列未对齐),本轮刻意未顺手改,以保持 diff 聚焦。

## 5. 下一步

E1 配置与事件模型:`config` 扩键面(单 `config.toml`)+ 事件模型冻结 + `action` 补下行契约段。
