# SyAgent-Project 工作区指南

本工作区由三个相互协作、但可独立构建的项目组成：

- `Open-LLM-VTuber/`：Python/FastAPI 实时语音交互与 Live2D 应用，承担 LLM、语音和直播业务编排。
- `onebot-gateway/`：Go WebSocket 多平台网关，接收 OneBot/哔哩哔哩事件并上传记忆服务。
- `bilibili-live/`：Rust 哔哩哔哩直播协议客户端与事件接入库。

修改前先确认目标子项目。若子目录中存在 `AGENTS.md`，以子目录规则为准；不要为了方便跨项目改动无关代码、配置或前端资源。

## 开发要求
1. 尽可能减少对项目的大幅改动
2. 使用中文的注释和日志的输出
3. 在相关的根目录使用git
4. 使用uv管理python项目 

## 工作区协作边界

典型数据流为：直播客户端（`bilibili-live`）或 QQ/哔哩哔哩适配端，将事件交给 `onebot-gateway`；网关按平台分发并异步上传；`Open-LLM-VTuber` 负责实时交互、模型、语音及 Live2D 侧能力。

- 跨项目改动先对齐事件 JSON、WebSocket 路径、平台名和配置键，再分别实现与验证。
- 不要提交密钥、Cookie、`SESSDATA`、API Key、`conf.yaml` 私有配置、聊天记录、缓存或模型权重。
- 保持变更聚焦；提交时仅暂存本任务涉及的文件。
- 各项目日志和注释优先使用中文；网络、反序列化等可恢复错误应记录上下文而非导致整个服务退出。

## Open-LLM-VTuber

核心后端位于 `Open-LLM-VTuber/src/open_llm_vtuber/`：`agent/` 负责 LLM/Agent，`asr/`、`tts/`、`vad/` 负责音频，`conversations/` 负责编排，`config_manager/` 负责 YAML 配置，`live/` 负责直播接入。入口为 `run_server.py`；除非任务明确涉及前端，否则不要修改 `frontend/`。

常用命令（在 `Open-LLM-VTuber/` 中运行）：

```bash
uv sync
uv run run_server.py --verbose
ruff check .
ruff format .
pre-commit run --all-files
```

- 使用 Python 3.10 兼容语法及现有异步模式。
- 新增引擎时，同时更新实现、工厂注册、配置模型和默认 YAML 模板。
- 变更后至少运行 `ruff check .`；涉及 WebSocket 或引擎时，以详细日志模式进行针对性验证。

## onebot-gateway

网关的事件入口是 `event.Dispatch(rawJSON)`：先解析路由字段，再解码具体事件，随后运行已注册的全部 handler，仅返回最后一个非零 `Action`。新增事件处理在 `app/register.go` 注册；多平台处理须保留并传递 `platform`，上传通知时使用 `senders.RegisterNotice(platform)`。

常用命令（在 `onebot-gateway/` 中运行）：

```bash
go build ./cmd/onebot-gateway/
go vet ./...
go test ./...
go mod tidy
```

- 文件名全小写且不使用下划线；导出标识符使用 `PascalCase`；JSON tag 使用 `snake_case`。
- 可选 JSON 字段使用指针加 `omitempty`，以区分未设置值。
- 用 `fmt.Errorf("描述: %w", err)` 包装错误；网络和解码错误应记录日志并尽量不中断处理链。
- 日志通过包级 `zap.NewNop()` 与 `SetLogger()` 注入；配置由 `ProvideConfig()` 从磁盘读取。
- `upload.Init(target)` 只在 `app.Initialize()` 初始化一次；`Upload` 与 `UploadNotice` 为 fire-and-forget 异步上传。

## bilibili-live

这是 Rust 2024 项目，依赖 Tokio、WebSocket、HTTP、Serde 与 Tracing。优先保持异步 I/O、结构化事件类型和现有错误传播风格；涉及协议时，先确认字段、压缩和签名处理与服务端实际格式一致。

常用命令（在 `bilibili-live/` 中运行）：

```bash
cargo fmt --check
cargo check
cargo test
cargo clippy --all-targets --all-features -- -D warnings
```

- 使用 `rustfmt` 格式化，避免无关重排。
- 公共协议结构使用 `serde` 明确序列化/反序列化字段；错误应保留根因并带有操作上下文。
- 新增 WebSocket 或任务逻辑时，避免阻塞 Tokio 运行时，并为断线、超时和取消路径提供可观察日志。

## 验证与交付

按改动范围运行最小充分的检查；跨项目变更至少验证编译/静态检查，并用可脱离外部凭据的单元测试或本地模拟验证事件契约。交付说明应写明修改的项目、验证命令及尚未验证的外部依赖。

## Agent skills

### Issue tracker

Issues and specs are tracked as local markdown files under `.scratch/<feature-slug>/`（本仓库根无 git remote，非 GitHub/GitLab）。See `docs/agents/issue-tracker.md`.

### Triage labels

Five canonical triage roles map to the label strings `needs-triage`、`needs-info`、`ready-for-agent`、`ready-for-human`、`wontfix`（本地 Markdown 后端的标签色值见调色盘表）。See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout — `CONTEXT.md` + `docs/adr/` at the repo root（不存在时静默继续，由 /domain-modeling 惰性创建）。See `docs/agents/domain.md`.
