# 切换判定与回滚预案

## 1. 现状

| 阶段 | 状态 |
| --- | --- |
| E0 骨架与基线 | ✅ |
| E1 配置与事件模型 | ✅ 配置键面映射见 `docs/CONFIG_MIGRATION.md` |
| E2 接入层 | ✅ QQ/OneBot、上行管线、**B 站事件由外部上报端经 `/bilibili` 接入**（网关不做协议） |
| E3 会话核心 | ✅ 会话管理、Agent 循环、句子分段、历史裁剪、角色资产 |
| E4 语音引擎 | ✅ 云 TTS 5 家 + failover，裸 PCM16 24kHz 单声道 |
| E5 播报队列 | ✅ 优先级/抢占/冷却/并行合成、`POST /api/speak`、distillery 已删 |
| E6 前端 | ✅ `/client-ws`、Live2D 页面、模型托管、表情与口型 |
| E7 记忆与工具 | ✅ JSON Lines 记忆 + 关键词召回、工具注册层、主动发言 |
| E8 集成与切换 | 🟡 本文档 + 联调结论；真实账号联调待执行 |

## 2. MVP 验收清单

对照执行计划决策票 10 的 MVP 范围，逐项给出判据与验证方式。

| # | 项目 | 判据 | 验证方式 |
| --- | --- | --- | --- |
| ① | B站弹幕 → 会话 → LLM → TTS → 前端 | 一条弹幕最终在浏览器里出声并显示字幕 | 已用「真二进制 + 假 LLM + 假 TTS + 真 WS 客户端」跑通：弹幕经 `/bilibili` 进入 → 创建 `room_123456` 会话 → 流式回复被切成 2 句 → 逐句入队（priority=danmaku）→ TTS 返回 0.6s PCM → 前端收到 `speak`（WAV base64、24kHz、PCM 编码）→ 回执后队列继续 |
| ② | QQ 群消息接入与上传 | 群消息进入会话并生成回复，Action 写回 | `go test ./internal/core/agent/conversation/` 覆盖；下行仅 QQ 群有 `send_group_msg` |
| ③ | `/api/speak` 播报 | `POST /api/speak` 入队并播报 | 见 `docs/INJECT_API.md`；冒烟已验证 200/400/405/503 与表情下标 |
| ④ | 统一播报队列 | 优先级、抢占、同级 FIFO、冷却 | `go test -race ./internal/core/agent/broadcast/` |
| ⑤ | 配置迁移 | 单文件 `config.toml` 覆盖旧三套配置 | 见 `docs/CONFIG_MIGRATION.md` |
| ⑥ | 前端真实渲染 | 浏览器里能看到 Live2D 角色并听到声音 | 无头 Chromium 实测：PixiJS + Cubism Core 初始化、模型渲染、字幕与音频播放、回执全通 |

## 3. 可重复的端到端冒烟

仓库自带一条不依赖任何真实凭据的冒烟：真二进制 + 假 LLM + 假 TTS + 真 WebSocket 客户端，
在临时目录里跑完「弹幕 → 会话 → LLM → 逐句播报 → TTS → 前端出声」并断言结果，结束后自动清理。

```bash
cd <仓库根>
scripts/e2e/run.sh
# 模型目录不在默认位置时：
SMOKE_MODELS_DIR=/path/to/live2d-models scripts/e2e/run.sh
```

脚本跑四条链路，每条都从「外部输入」一路验到「浏览器收到音频」：

| 轮次 | 输入 | 断言 |
| --- | --- | --- |
| 1 | 普通弹幕 | 流式回复被切成 2 句，逐句播报（priority=danmaku） |
| 2 | 触发工具调用的弹幕 | `memory_search` 真的执行、结果回灌、最终答复成句 |
| 3 |  `POST /api/speak`| 注入播报（priority=proactive）且表情下标正确（joy → 3） |
| 4 | 静默 2 秒 | 主动发言，最低优先级（priority=idle），任何弹幕都能打断它 |

每条播报都会校验音频是合法的 24kHz 单声道 PCM WAV，并回执 `playback-finished`——队列要等回执
才继续，不回执后面全都会卡住。跑完还会校验网关日志与落盘结果（记忆文件、TTS 调用），
并检查 `工具 memory_search 执行完成`／`播报注入已入队`／`主动发言已入队` 三行日志。

最后一轮验证关停：脚本给网关发 SIGTERM，确认它在 5 秒内退出、日志出现
「vtuber-agent-go 已退出」、端口真的不再响应——这条路径（`App.Run` 的 ctx 取消分支）
只有真实信号才跑得到。

跑完这四轮后，如果模型目录存在且环境里有 playwright，会再开一次无头 Chromium 检查页面本身：
遮罩先挡住、点击后才连、模型真的渲染出尺寸、注入一句后字幕出现、控制台没有报错。
没有 playwright 时这一步打印「跳过」并以成功退出，不影响前面的结论。

模型目录不存在时会自动跳过前端接入与浏览器检查，只验证到播报队列。

## 4. 切换前必须完成的真实联调

以下项目需要真实凭据，仓库内的测试无法覆盖：

1. **B 站上报端**：网关不含 B 站协议实现——需要外部上报端连到 `[[clients]]` 里 `/bilibili` 这个地址，
	把事件信封原样上报。上报端要求与验收清单见 `docs/BILIBILI_INGEST.md`（原 Rust 客户端有 5 处实证缺陷：
	压缩帧事件被静默丢弃、重连实际不可达、帧长越界 panic 等，若选它当上报端必须逐条验收）。
2. **LLM**：填入 `[llm]` 的真实端点与模型，确认流式回复与（开启工具后的）工具调用。
3. **TTS**：从 `[tts].engines` 里选一个可用引擎并填凭据；`edge_tts` 免凭据但需要外网。
4. **前端**：浏览器打开 `http://<host>:6199/web/`，点「点击开始」，确认模型加载与出声。
5. **QQ 侧**（可选）：OneBot 实现连到 `[[clients]]` 里配置的路径。

## 5. 切换步骤

```bash
# 1. 停掉旧系统
#    - Open-LLM-VTuber（:12393）
#    - 若仍在跑：llm-vup-bridge（:9528）
#    B 站上报端（bilibili-live 或其替代）**不停**：它现在的角色是给本服务喂事件，
#    确认它指向本服务的 /bilibili，而不是旧系统地址。

# 2. 起网关（单二进制，配置与角色资产在同一目录）
cd <仓库根>
go build -o vtuber-agent-go ./cmd/vtuber-agent-go/
./vtuber-agent-go

# 3. 浏览器打开前端
xdg-open http://127.0.0.1:6199/web/
```

启动日志应当包含：`已加载角色`、`前端已就绪`、`长期记忆已启用`（若配置）、
`语音播报已启用`、`agent 会话已启用`。

## 6. 回滚预案

Go 版与旧系统的数据面互不依赖（不共用数据库、不共用配置文件），因此回滚就是**重启旧系统**：

| 项 | 回滚动作 |
| --- | --- |
| 进程 | 停 `vtuber-agent-go`，按旧流程起 `Open-LLM-VTuber` + `bilibili-live`（B 站上报端需改回连旧系统地址） |
| 配置 | 旧三套配置（`conf.yaml` / 旧 `config.toml` / `config.json`）从未被 Go 版改写，直接可用 |
| 前端 | 旧前端是独立的 Vue 产物，随 `Open-LLM-VTuber` 一起回来（该仓库已移出本仓，位于 `~/Project/Open-LLM-VTuber`） |
| 数据 | Go 版只新增了 `[agent].memory_file` 指向的 JSON Lines 文件，删掉即可；旧系统的数据未被触碰 |
| 模型资产 | Go 版只读 `live2d-models/` 与 `model_dict.json`，不写入 |

**观察窗口 24 小时**：切换后观察一天，确认弹幕→回复时延、播报不重叠、断线重连正常、
内存与历史长度稳定（历史裁剪与记忆文件的条目数都不应无界增长）。

## 7. 已知差异与遗留

| 项 | 说明 |
| --- | --- |
| ASR / VAD | 整包不做（直播场景没有麦克风输入源），旧配置对应键面已删除 |
| 本地推理引擎 | 全部不做（含 sherpa，避免 CGo）；TTS 只保留 5 家云引擎 |
| 旧前端的 21 种消息 | 不复刻；自研页面只认 2 种下行与 2 种上行，见 `internal/backend/web/doc.go` |
| 翻译预处理 | 未实现（`tts_preprocessor_config` 一系键面已删除） |
| `[memory].target` | 已删除：事件终端改为进程内调用，不再有远端 WebSocket |
| 记忆存储 | 由 SQLite 改为追加式 JSON Lines，理由见 `internal/core/agent/memory/memory.go` 包注释 |
