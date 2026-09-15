# AGENTS.md — onebot-gateway

WebSocket 多平台网关，接收 QQ/B站 等客户端上报的 OneBot 事件，分发到已注册的处理器链，并异步将群消息/通知上传到外部记忆服务。

**所有的注释和日志都使用中文**


## 模块划分

单 `go.mod`,顶层目录即模块边界;依赖只能向下,反向用函数注入破环(先例 `gateway/upload.SetActionForwarder`),不引入 DI 容器:

```text
app/                 装配:config → logger → 注入 → 注册 handler → 路由
gateway/             接入层:只认平台协议,不认会话/LLM
  event/               平台分发(event.Dispatch)+ onebot/ + bilibililive/(纯数据)
  bilibili/            B站开放平台 WSS 客户端(只放连接,不放事件字段)
  server/              WS 接入与 Action 写回
  upload/              上行管线(暂存区/队列/长连接/回调)
  filter/              去重 + 敏感词
  distillery/          过渡期语音反馈转发(接入 broadcast 后删除)
agent/               编排层:只认事件与会话,不认平台协议
  conversation/        会话/LLM 编排
    llm/               OpenAI 兼容端点的流式客户端(基于 go-openai,无状态)
  broadcast/           统一播报队列(优先级/打断/TTS 任务)
  memory/              SQLite 历史与关键词召回
  frontend/            /client-ws 与静态资源托管
  tool/                Tool 注册层
tts/                 云 TTS 引擎(5 家 + failover,不做本地推理)
shared/              跨模块契约(零内部依赖);`shared/action` 为下行 Action 契约
config/ logger/      基础设施
```

接口归调用方所有:agent 定义需要什么,gateway/tts 提供实现;反向只走 `SetXxxFunc`。
## 常用命令

```bash
go build ./cmd/onebot-gateway/   # 编译
./onebot-gateway                 # 运行（需要 config.toml 在当前目录）
go vet ./...                     # 静态检查
go test ./...                    # 测试（尚无测试用例）
go mod tidy                      # 清理依赖
```

## 核心模式

### 事件分发（最重要）

`event.Dispatch(rawJSON)` 是分发入口。流程：

1. 两阶段解析：先解 `post_type`+`sub_type` 等路由字段（`eventTypeProbe`），再完整解码到具体事件类型
2. 泛型 `ActionList[T].Run(event)` 遍历所有注册 handler — **所有 handler 都执行**，但只返回**最后一个非零 Action**
3. 零 Action 判定：`Action=="" && Params==nil && Echo==nil`

添加新事件处理：在 `app/register.go` 中调用 `event.XXXActions.Add(func(e XXXEvent) action.Action { ... })`。
`senders.RegisterNotice()` 需传入 `platform` 参数以正确标记上传来源。

### 多平台架构

配置中 `[[clients]]` 数组定义每个接入客户端，各自绑定独立 WebSocket 路径：

```toml
[[clients]]
platform = "qq"
adapter_key = "onebot_v11"
path = "/qq"

[[clients]]
platform = "bilibili"
adapter_key = "bilibili_live"
path = "/bilibili"
```

- `server` 包为每个 `client.Path` 注册路由，通过 `context.WithValue` 传递平台标识
- `app/register.go` 中 `registerQQ()` / `registerBilibili()` 分别注册各平台 handler
- 上传时 `Upload(ctx, platform, event)` / `UploadNotice(ctx, platform, userID, text)` 的 `platform` 参数写入 `platformEvent.PlatformName`

### 事件上传

`upload/client.go`（包名 `upload`）：

- `Init(target, callbackPlatform, Options)` 在 `app.Initialize()` 中调用一次，建立到记忆服务的 WebSocket 长连接（后台自动重连）；Options 承载缓存容量、等待上限、敏感词库、去重窗口等配置
- 上传管线从前到后：去重+敏感词过滤（`gateway/filter`）→ 有界缓存背压（`queue_size`/`queue_wait_timeout`，满时阻塞入队、超时丢弃并记录错误日志）→ Action 顺序门控（`BeginDispatch`/`FinishDispatch`，分发期间暂存，Action 给出后才放行）→ 远程写锁（`writeMu`，同一时刻只有一个在途上传）
- `Upload(ctx, platform, event)` / `UploadNotice(ctx, platform, userID, text)` 将 `platformEvent` JSON 序列化后进入上传管线，不等待远端响应
- 目标地址由 `config.toml` 中 `[memory].target` 指定，格式 `ws://host:port/path`；为空时 `Init` 仅记录错误并跳过，上传直接丢弃

### Logger 注入

每个需要日志的包用包级 `var log = zap.NewNop()` + `SetLogger()` 注入。调用方在 `app.Initialize()` 中注入。

## 约定

| 项        | 规则                                                                   |
| --------- | ---------------------------------------------------------------------- |
| 命名      | 文件名全小写无下划线；导出标识符 PascalCase；JSON tag snake_case       |
| JSON 字段 | 可选字段用指针（`*string`/`*int64`/`*bool`）+ `omitempty` 区分"未设置" |
| 错误处理  | `fmt.Errorf("描述: %w", err)` 包装；网络/解码错误记录日志不中断        |
| 日志      | 调试用 `log.Sugar().Debugf`，错误用 `log.Sugar().Errorf`               |
| 配置      | `ProvideConfig()` 每次从磁盘重读，允许运行时改配置                     |

## 外部依赖

- QQ 侧：OneBot v11 客户端（配置 `[[clients]]` + `[server].addr`）
- B站 侧：bilibili live 转 OneBot 格式客户端
- 记忆服务：WebSocket 端点（配置 `[memory].target`，接收 `platformEvent` JSON）
