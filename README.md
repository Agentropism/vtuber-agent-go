# vtuber-agent-go

实时语音交互 VTuber 的单二进制 Go 实现：接入 QQ / B站 事件 → 会话与 LLM → 统一播报队列 → 云 TTS → 内置 Live2D 前端。

```text
QQ 适配端 ─┐                                    ┌→ 文本回复写回原平台
           ├→ internal/gateway → internal/agent ─┤
B站 长连接 ─┘   (分发/去重/上行管线)   (会话/LLM)  └→ 播报队列 → internal/tts → 前端出声
```

## 快速开始

```bash
go build ./cmd/vtuber-agent-go/   # 编译
cp config.toml.example config.toml # 配置（含凭据，不进版本控制）
./vtuber-agent-go                 # 运行；config.toml 与 characters/ 需在当前目录
xdg-open http://127.0.0.1:6199/web/
```

验证：

```bash
gofmt -l . && go build ./... && go vet ./... && go test ./...
scripts/e2e/run.sh                # 端到端冒烟（真二进制 + 假 LLM/假 TTS + 真 WS 客户端，不需要真实凭据）
```

## 目录

```text
cmd/vtuber-agent-go/   入口（单二进制；唯一不在 internal/ 下的包）
internal/              app / gateway / agent / tts / stream / shared / config / logger
characters/            角色资产（人设 + Live2D 模型映射）
scripts/e2e/           端到端冒烟脚本
docs/                  契约文档；docs/archive/ 为归档文档；docs/agents/ 为协作流程定义
```

模块边界与依赖方向见 [AGENTS.md](AGENTS.md)；对外契约见 `docs/`：

| 文档 | 内容 |
| --- | --- |
| [`docs/EVENT_CONTRACT.md`](docs/EVENT_CONTRACT.md) | 事件信封、`platformEvent` 上传格式、事件种类 |
| [`docs/CLIENT_INTEGRATION.md`](docs/CLIENT_INTEGRATION.md) | 接入客户端如何连上来、上报什么 |
| [`docs/INJECT_API.md`](docs/INJECT_API.md) | `POST /inject` 播报注入 |
| [`docs/MEMORY_API.md`](docs/MEMORY_API.md) | 长期记忆文件格式与召回 |
| [`docs/CONFIG_MIGRATION.md`](docs/CONFIG_MIGRATION.md) | 旧三套配置到 `config.toml` 的逐键映射 |
| [`docs/CUTOVER.md`](docs/CUTOVER.md) | 切换判据、冒烟、真实联调与回滚预案 |

## 约定

- 注释与日志一律中文。
- 单 `go.mod`，除 `cmd/` 外全部包在 `internal/`；依赖只能向下，反向用函数注入破环。
- 纯 Go、无 CGo，产物是单个二进制；前端页面用 `go:embed` 内嵌。
- 凭据只走环境变量或 `config.toml`（已 gitignore），不进仓库。
- B 站事件由**外部上报端**经 `/bilibili` 上报：网关不含 B 站协议实现（连接、鉴权、重连、凭据都在上报端）。接入要求与已知坑见 [`docs/BILIBILI_INGEST.md`](docs/BILIBILI_INGEST.md)。
- 推流（可选）：配 `[stream]` 后由本进程起 Xvfb + Chrome 渲染 `/web/` 画面，ffmpeg 抓屏混音推 RTMP；地址可自动开播获取，也可手填 `output`。
