# AGENTS.md — vtuber-agent-go

单二进制 VTuber 交互服务：接入 QQ/B站 客户端上报的 OneBot/直播事件，分发到已注册的处理器链，把群消息/弹幕异步交给会话 Agent 生成回复，回复经统一播报队列合成语音后推给内置 Live2D 前端。

**所有的注释和日志都使用中文**


## 模块划分

单 `go.mod`（module `github.com/Agentropism/vtuber-agent-go`）；**除 `cmd/` 外全部包位于 `internal/`**，`internal/` 下的目录即模块边界。依赖只能向下，反向用函数注入破环（先例 `internal/gateway/upload.SetHandler`），不引入 DI 容器。

```text
cmd/vtuber-agent-go/  入口(单二进制;唯一不在 internal/ 下的包)
internal/app/        装配:config → logger → 注入 → 注册 handler → 路由
internal/gateway/    接入层:只认平台协议,不认会话/LLM
  event/               平台分发(event.Dispatch)+ onebot/ + bilibililive/(纯数据)
  server/              WS 接入与 Action 写回 + POST /inject 播报注入
  upload/              事件上行管线(去重/敏感词 → 背压 → 分发门控),出口由 SetHandler 注入
  filter/              去重 + 敏感词
internal/agent/      编排层:只认事件与会话,不认平台协议
  conversation/        会话编排:按 channel_id 的会话管理 + 单会话 Agent(上传管线的终端)
    llm/               OpenAI 兼容端点的流式客户端(基于 go-openai,无状态)
  broadcast/           统一播报队列(优先级/抢占/冷却/并行合成),Synthesizer+Sink 注入
  memory/              长期记忆：追加式 JSON Lines + 关键词（二元组）召回
  frontend/            /client-ws 协议、Live2D 页面与模型托管、播报 Sink
  tool/                Tool 注册层
internal/tts/        云 TTS 引擎(5 家 + failover,不做本地推理);统一输出裸 PCM16 24kHz 单声道
internal/stream/     推流层:开播取地址(B站 web 接口)+ Xvfb/Chrome 渲染 + ffmpeg 抓屏混音 → RTMP;由 [stream] 驱动
internal/shared/     跨模块契约(零内部依赖);`action` 为下行 Action 契约
internal/config/ internal/logger/  基础设施
characters/ scripts/ config.toml.example   非 Go 资产,与 Go 包同层;前端静态页面在 internal/agent/frontend/web/(go:embed,必须留在包内)
```

接口归调用方所有:agent 定义需要什么,gateway/tts 提供实现;反向只走 `SetXxxFunc`。
## 常用命令

```bash
go build ./cmd/vtuber-agent-go/  # 编译
./vtuber-agent-go                # 运行（需要 config.toml 在当前目录，见 config.toml.example）
go vet ./...                     # 静态检查
go test ./...                    # 测试
go mod tidy                      # 清理依赖
```

## 核心模式

### 事件分发（最重要）

`event.Dispatch(rawJSON)` 是分发入口。流程：

1. 两阶段解析：先解 `post_type`+`sub_type` 等路由字段（`eventTypeProbe`），再完整解码到具体事件类型
2. 泛型 `ActionList[T].Run(event)` 遍历所有注册 handler — **所有 handler 都执行**，但只返回**最后一个非零 Action**
3. 零 Action 判定：`Action=="" && Params==nil && Echo==nil`

添加新事件处理：在 `internal/app/register.go` 中调用 `event.XXXActions.Add(func(e XXXEvent) action.Action { ... })`。
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
- `internal/app/register.go` 中 `registerQQ()` / `registerBilibili()` 分别注册各平台 handler
- 上传时 `Upload(ctx, platform, event)` / `UploadNotice(ctx, platform, userID, text)` 的 `platform` 参数写入 `platformEvent.PlatformName`
- **网关不含 B 站协议实现**：连开放平台、鉴权、心跳、重连、拆帧都由**外部上报端**负责，它经 `/bilibili` 上报事件信封；凭据配在上报端自己那里。上报端要求与已知坑见 `docs/BILIBILI_INGEST.md`（2026-09 删除了原内置 Go 客户端 `internal/gateway/bilibili`，它的实测缺陷清单已转成该文档的验收清单）

### 事件上行与 agent 接入

`upload/client.go`（包名 `upload`）：

- `Init(Options)` 在 `app.Initialize()` 中调用一次；Options 承载缓存容量、等待上限、敏感词库、去重窗口等配置
- 事件终端由 `SetHandler(fn func(payload []byte))` 注入，**进程内异步方法调用**，不再连远端 WebSocket；未注入时事件被丢弃并记录日志
- 管线从前到后：去重+敏感词过滤（`internal/gateway/filter`）→ 有界缓存背压（`queue_size`/`queue_wait_timeout`，满时阻塞入队、超时丢弃并记录错误日志）→ Action 顺序门控（`BeginDispatch`/`FinishDispatch`，分发期间暂存，Action 给出后才放行）→ 专用消费协程串行调用终端处理器
- `Upload(ctx, platform, event)` / `UploadNotice(ctx, platform, userID, text)` 将 `platformEvent` JSON 序列化后进入管线，不等待处理结果
- 终端的实现是 `conversation.Sessions.Handle`：解析 `platformEvent` → 按 `channel_id` 路由到会话 → 会话协程内跑 `Agent.Chat` → 用注入的 `ReplyFunc`（`server.SendAction`）把回复写回平台

回复方向由 `internal/agent/conversation.buildReply` 决定：目前只有 QQ 群有下行动作（`send_group_msg`），B 站弹幕没有发送接口、只生成不发送。

### 播报链路

事件种类（`platformEvent.event_kind`，见 `internal/shared/event`）同时决定两件事：是否需要 LLM 回复、以及播报优先级（`broadcast.PriorityFor`，SC > 礼物/大航海 > 弹幕与群消息）。

```text
会话回复文本 ──┬─→ ReplyFunc（server.SendAction）→ 原平台
              └─→ Broadcaster.Enqueue ─┐
POST /inject ─────────────────────────┴─→ 播报队列 ─→ tts.Chain 合成 ─→ Sink.Play
```

- 播报有两个入口，共用同一条队列：会话回复（逐句入队）与 `POST /inject`（外部程序注入，`internal/gateway/server` 收下后交给队列）。注入不经过 LLM，文本原样播报，入队即返回；契约见 `docs/INJECT_API.md`。
- `internal/agent/conversation.Sessions` 只做入队，不等待合成；播报入队失败只记日志，不影响文本回复。
- 回复**逐句**入队（`conversation.Splitter`）：流式生成过程中一旦断出完整句子就立刻入队，首字延迟取决于第一句而不是整段回复；首句额外允许在逗号处提前断开（`firstClauseMinLength`）。文本下行仍用完整回复。
- `tts.Chain` 按 `[tts].engines` 顺序降级，引擎统一返回**裸 PCM16 24kHz 单声道**（无容器头），采样率不一致时在包内重采样。
- `broadcast.Queue` 需要注入 `Synthesizer`（传 `tts.Chain`）与 `Sink`。配了 `[frontend]` 时 `Sink` 是 `internal/agent/frontend`：把裸 PCM 补上 WAV 头 base64 后推给浏览器，并**等前端回执**（放完才继续下一句）；没配前端时退回 `logSink`，只记录并按时长等待。
- `[tts].engines` 为空时整个播报链路不装配，会话只做文本下行，`/inject` 返回 503。

### 前端接入

`internal/agent/frontend` 是 `broadcast.Sink` 的实现，同时提供三个路由，由 `app` 经 `server.Route` 挂上：

| 路径 | 内容 |
| --- | --- |
| `/web/` | 内置的极简 Live2D 页面（`go:embed`，随二进制分发） |
| `/client-ws` | 浏览器长连接：下发 `hello`/`speak`，回收 `playback-started`/`playback-finished` |
| `/live2d-models/` | 模型静态资源；`/live2d-models/info` 返回模型清单 |

- 页面挂在 `/web/` 而不是 `/`：接入客户端可以占用根路径（`[[clients]].path`），两者不该抢路由；`server.ProvideServer` 对重复 pattern 会跳过并报错，不会 panic。
- 表情：模型清单的 `emotionMap` 决定词表，对话侧把回复里的 `[joy]` 标签摘进 `broadcast.Item.Emotion`，前端换成 Live2D 表达式下标；词表定义在 `internal/shared/emotion`。
- 音频契约：TTS 输出裸 PCM16 24kHz 单声道，由 `internal/agent/frontend` 补 44 字节 WAV 头再 base64——浏览器不吃裸 PCM。

### 推流（替代 OBS）

配了 `[stream].enabled` 后由 `app.Run` 起停，链路是：

```text
开播取地址(B站 web 接口) → Xvfb 虚拟屏 → Chrome 全屏跑 /web/?autostart=1（画面来源）
播报队列的 PCM ──────────────→ 命名管道 ──┐
屏幕画面 ────────────────────→ x11grab ──┴→ ffmpeg → flv → RTMP
```

- 地址来源二选一：`cookie` + `room_id` 调开播接口自动拿（退出时自动关播），或直接填 `output`。
- **登录态怎么来**：起服务后浏览器打开 `http://127.0.0.1:<端口>/login/` 扫码（推荐），登录态落到 `data/bilibili-cookie.txt`（0600）；也可以直接填 `[stream].cookie` 或环境变量 `BILIBILI_COOKIE`。配置优先于扫码结果。
- 登录页三个端点（`/login/`、`/login/qrcode.png`、`/login/status`）**只允许回环地址访问**：二维码一被扫就绑定账号，不该暴露给局域网。二维码由后端出 PNG（`go-qrcode`），页面本体不依赖任何前端库。
- 首次使用的顺序必然是「先起服务 → 扫码 → 推流」：没登录态时 `waitForCookie` 会等（每 5s 重试）并打出可点的登录地址，而不是启动失败。
- 推流与浏览器 Sink 是**并行扇出**（`pickSink` / `fanOutSink`），不是二选一：Chrome 里那个页面仍要靠 `speak` 驱动口型与字幕。
- 空闲时 `silenceKeepalive` 补静音：命名管道没有写端时 ffmpeg 会阻塞在读音频上，连视频一起停（没配 TTS 就会撞上）。
- 自检不需要任何凭据：`input = "test"` + `output = "/tmp/x.flv"`，跑完用 `ffprobe` 看 h264/aac 轨。

踩过的四个坑（代码注释里都留了记号）：残留的 X socket 会被误判成活屏（`displayAlive` 真连一次而不是 stat）、ffmpeg 缺 `-y` 时上一轮残留的输出文件会让重启永远失败、Wayland 会话下 Chrome 会连 Wayland 而绕过虚拟屏（必须 `--ozone-platform=x11`）、虚拟屏没有 GPU 要显式放开软件 WebGL（`--enable-unsafe-swiftshader`）。
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
- B站 侧：**外部上报端**经 `/bilibili` 上报（`[[clients]]` + `adapter_key = "bilibili_live"`）；网关不做协议、不存凭据，接入要求见 `docs/BILIBILI_INGEST.md`
- LLM：OpenAI 兼容远程端点（配置 `[llm]`，凭据可用环境变量 `LLM_API_KEY`）；未配置时跳过会话初始化，事件被丢弃
- TTS：云引擎（配置 `[tts].engines` 与各引擎子表，凭据可用 `OPENAI_API_KEY` / `SILICONFLOW_API_KEY` / `FISH_API_KEY` / `MINIMAX_API_KEY`）；`edge_tts` 无需凭据。留空表示不启用语音播报
- 推流（可选）：要 `ffmpeg`，`[stream].renderer` 打开时还要 `Xvfb` 与 Chrome（默认 `google-chrome-stable`）。

## 仓库布局与文档

```text
cmd/ internal/            Go 代码（cmd 是唯一非 internal 的包）
characters/ scripts/e2e/  角色资产与端到端冒烟脚本
config.toml.example       配置模板；config.toml 与 data/ 是运行时产物，已 gitignore
docs/                     活契约（CUTOVER / EVENT_CONTRACT / INJECT_API / CLIENT_INTEGRATION / CONFIG_MIGRATION / MEMORY_API）
  agents/                 协作流程定义（issue tracker / triage labels / domain docs）
  archive/                已归档文档（go-rewrite effort 与历史设计）
```

依赖方向：`app → {gateway, agent, tts} → shared`；`gateway → agent` 单向。

## Agent skills

### Issue tracker

Issues and specs are tracked as local markdown files under `docs/<feature-slug>/`（后端为本地 Markdown，不走 GitHub Issues）。
See `docs/agents/issue-tracker.md`.

### Triage labels

Five canonical triage roles map to the label strings `needs-triage`、`needs-info`、`ready-for-agent`、`ready-for-human`、`wontfix`。
See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout — `CONTEXT.md` + `docs/adr/` at the repo root（不存在时静默继续，由 /domain-modeling 惰性创建）。
See `docs/agents/domain.md`.
