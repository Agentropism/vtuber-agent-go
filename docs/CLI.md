# 命令行契约

单个二进制 `vtuber-agent-go` 的子命令契约：参数、退出码、输出格式与 `--json` 字段名。

**裸跑不启动服务**：不带子命令直接执行只打印帮助（退出码 1），启动服务用 `run`。

| 项 | 约定 |
| --- | --- |
| 命令集 | `run` / `config` / `doctor` / `version`；另有 `help`（cobra 内置） |
| 参数风格 | cobra；`--config`、`--json` 是**全局（persistent）参数**，所有子命令都认 |
| 退出码 | `0` = 成功，`1` = 失败（含用法错误）。**不设三档**：警告不影响退出码，只有失败才非零 |
| 输出 | 默认人读文本；`--json` 时**只输出 JSON**，不带任何提示语（可被脚本直接解析） |
| 颜色 | 仅在 stdout 是 TTY 且未设 `NO_COLOR` 时启用；`--json` 下强制关闭 |
| 相对路径 | 配置里的相对路径（`characters/`、`data/` 等）一律**以当前工作目录为基准**，与主程序一致；`--config` 只决定配置文件在哪，不改变基准 |
| 平台 | 只保证 Linux / WSL |

## 全局参数

| 参数 | 默认 | 含义 |
| --- | --- | --- |
| `--config <路径>` | `./config.toml` | 配置文件路径；`run` / `config` / `doctor` 都认 |
| `--json` | 关 | 输出 JSON（字段名见下，属对外契约） |
| `--help` | — | 帮助文本为中文 |

```bash
vtuber-agent-go run                      # 启动服务（原来的裸跑行为）
vtuber-agent-go config init              # 生成 config.toml
vtuber-agent-go doctor                   # 离线自检
vtuber-agent-go doctor --json            # 机器可读，退出码同文本模式
vtuber-agent-go -c /etc/agent/config.toml run
```

只给到父命令（如 `config`）而不给子命令时，与裸跑一致：打印帮助并以 `1` 退出——「没给出要做的动作」不算成功。

## run

装配并启动服务：HTTP 服务、事件接入、会话与 LLM、播报队列、内置前端。收到 `SIGINT` / `SIGTERM` 后
优雅退出（停服务、收尾连接），最后打印 `vtuber-agent-go 已退出`。除全局参数外不接受其它参数。

配置文件缺失或非法、角色文件读不到、模型目录不存在等情况会在启动阶段直接失败（退出码 1）。

## config

### config init

把 `./config.toml.example`（相对**当前工作目录**，不是 `--config` 所在目录）复制到 `--config` 指定的路径。

```bash
vtuber-agent-go config init              # 生成 ./config.toml
vtuber-agent-go config init --force      # 已存在时覆盖
```

- 生成的权限是 `0600`：里面迟早会填 API key 与登录态。
- 目标文件已存在且未给 `--force` 时**报错退出，不覆盖**（防手滑洗掉凭据）。
- `./config.toml.example` 不存在时报错退出并说明来源——模板不做内嵌副本，避免出现第二份事实源。
- 只做复制，**不校验**：配置合法性由 `doctor` 负责。

`--json`：

```json
{"path": "config.toml", "source": "config.toml.example", "created": true}
```

### config show

打印当前**生效**的配置快照（含环境变量补齐的凭据状态）。人读模式输出 TOML（键名与 `config.toml`
同构，便于对照），`--json` 输出与 [`/api/config`](API.md#1-配置只读) **完全同构**的 JSON。

脱敏规则与后端共用同一份实现：`api_key`、`cookie` 只回 `{"configured": bool}`，永不回明文；
`cookie_file` 是路径不是凭据，照常回字符串。

```bash
vtuber-agent-go config show              # TOML
vtuber-agent-go config show --json       # 与 GET /api/config 同构
```

## doctor

**离线自检**：不联网、不建目录、不开文件、不外呼（端口探测会瞬时 bind 后立即释放）。
用来把「启动到一半才报错」提前到启动之前。配置校验只此一处，`config` 不重复做。

检查项（`name` 是契约名）：

| name | 检查内容 | 失败条件 |
| --- | --- | --- |
| `config` | 配置文件能否解析；`[server].addr` 非空；`[llm].base_url` / `[llm].model` 非空；`[tts].engines` 里每个引擎名都有对应的 `[tts.*]` 段且必填项齐全 | 任一项不满足 |
| `character` | `[agent].character_file` 可读、TOML 合法、能解析出角色名与 Live2D 模型名 | 文件读了但解析失败；文件为空时算 `warn`（改用内置兜底提示词） |
| `paths` | `[agent].memory_file` / `[agent].archive_dir` 的最近已存在祖先目录可写；已存在的 `memory.jsonl` 每行都是合法 JSON | 目录不可写或既有数据损坏 |
| `port` | 对 `[server].addr` 做一次 bind 探测（立即释放） | 端口已被占用 |
| `binaries` | `[stream].enabled` 时查 `ffmpeg`；`renderer = true` 且 `input != "test"` 时另查 `Xvfb` 与 `chrome`。名字取配置字段，留空回退 `ffmpeg` / `Xvfb` / `google-chrome-stable` | 必需的可执行文件不在 `PATH` |
| `credentials` | 只判「有没有配」（配置值或对应环境变量），**不验证有效性**：LLM 的 `api_key`；`[tts].engines` 列出引擎的 key；`[stream].enabled` 且未手填 `output` 时的 B 站登录态 | 必填凭据缺失 |
| `credentials` | 只判「有没有配」（配置值或对应环境变量），**不验证有效性**：LLM 的 `api_key`；`[tts].engines` 列出引擎的 key；`[stream].enabled` 且未手填 `output` 时的 B 站登录态 | LLM 与已启用引擎的凭据缺失算失败；**B 站登录态缺失只算警告**（开播前可在 `/login/` 扫码补，app 允许先启动） |

未启用的能力（没配 `[tts].engines`、没开 `[stream]`、没配 `[frontend]`）= `skip`，不是失败。
`node` / `python3` 不是运行时依赖（只有 `go generate` 与构建期需要），**不检查**。

`status` 取值：`pass` / `warn` / `fail` / `skip`。

```bash
vtuber-agent-go doctor                   # 人读
vtuber-agent-go doctor --json            # 见下
```

`--json`：

```json
{
  "ok": false,
  "config": "config.toml",
  "checks": [
    {"name": "config", "status": "pass", "message": "config.toml 解析通过"},
    {"name": "paths", "status": "warn", "message": "未配置 [agent].memory_file，跳过长期记忆"},
    {"name": "port", "status": "fail", "message": "127.0.0.1:6199 已被占用", "details": {"addr": "127.0.0.1:6199"}}
  ],
  "summary": {"pass": 4, "warn": 1, "fail": 1, "skip": 1}
}
```

- `ok` = 没有任何 `fail`（有 `warn` 仍为 `true`，退出码 0）。
- `checks` 顺序固定，与上表一致。
- `details` 是可选对象，**其内部字段不承诺稳定**（仅供人查/排障）；稳定的是 `name` / `status` / `message`。

## version

输出四项：版本号、commit、构建时间、Go 版本。源码里有默认值（版本 `dev`，其余为空），发布时用
`-ldflags` 注入：

```bash
go build -ldflags "-X github.com/Agentropism/vtuber-agent-go/internal/cli.Version=1.2.3 \
  -X github.com/Agentropism/vtuber-agent-go/internal/cli.Commit=$(git rev-parse --short HEAD) \
  -X github.com/Agentropism/vtuber-agent-go/internal/cli.BuildTime=$(date -Is)" \
  ./cmd/vtuber-agent-go
```

未注入的字段人读显示「未知」，`--json` 里是空字符串——**不造假值**。

```json
{"version": "dev", "commit": "", "build_time": "", "go_version": "go1.26.4"}
```

## 契约稳定性

`--json` 的字段名、`doctor` 的 `checks[].name` 与 `status` 取值、退出码语义都是对外契约：
**允许新增字段与新增检查项，改名或删除算破坏性变更**，需要同批更新本文件。
`details` 内部字段不在此列。
