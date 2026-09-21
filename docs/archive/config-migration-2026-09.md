# 配置迁移指南：三套配置 → 单一 config.toml

> **已归档（2026-09-21）**：这是三套配置 → 单 `config.toml` 一次性迁移过程的记录，里面引用的
> 旧系统（Open-LLM-VTuber / llm-vup-bridge）与文档路径按当时状态保留，不再维护。
> 当前配置契约见 `config.toml.example` 与 `docs/API.md`。


## 1. 概述

旧系统有三套配置文件，各自的键面互不相识：

| 旧文件 | 归属 | 内容 |
| --- | --- | --- |
| `Open-LLM-VTuber/conf.yaml` | Python | 服务端口、角色人设、Agent/LLM/ASR/TTS/VAD、翻译、前端开关 |
| `config.toml`（仓库根） | Go | 服务端口、接入客户端、事件上传目标 |
| `llm-vup-bridge/config.json` | Go（已删除） | 转发端点、LLM 意图分析、发言冷却 |

Go 版把三者收敛为 **`config.toml` + `characters/*.toml`**（都在仓库根）：
部署参数进 config.toml，角色人设进角色文件（见 `characters/mili.toml`）。

本文给出逐键映射与删除理由。已实现的键以 `config.toml.example` 为准。

## 2. 已迁移的键

### 2.1 服务与角色

| conf.yaml | config.toml | 说明 |
| --- | --- | --- |
| `system_config.host` / `port` | `[server].addr` | 合并成一个地址串，默认 `:6199`（网关端口） |
| `system_config.config_alts_dir` | — | 角色目录固定为 `characters/`，用 `[agent].character_file` 选具体文件 |
| `character_config.persona_prompt` | `characters/*.toml` 的 `system_prompt` | 人设优先取角色文件，其次 `[agent].system_prompt` |
| `character_config.character_name` | `characters/*.toml` 的 `name` | 前端显示名 |
| `character_config.avatar` | `characters/*.toml` 的 `avatar` | 头像文件名 |
| `character_config.live2d_model_name` | `characters/*.toml` 的 `live2d_model` | 前端加载的模型目录名 |
| `character_config.conf_name` / `conf_uid` | — | 角色文件路径本身就是身份，不再需要 UID |

### 2.2 LLM 与 Agent

| conf.yaml | config.toml | 说明 |
| --- | --- | --- |
| `...llm_configs.<provider>.llm_api_key` | `[llm].api_key` | 只保留 OpenAI 兼容协议；凭据可用环境变量 `LLM_API_KEY` |
| `...llm_configs.<provider>.base_url` | `[llm].base_url` | 同上 |
| `...llm_configs.<provider>.model` | `[llm].model` | 同上 |
| `...llm_configs.<provider>.temperature` | `[llm].temperature` | 同上 |
| `...llm_configs.<provider>.interrupt_method` | `[agent].interrupt_mode` | `user`（默认）/ `system` |
| `agent_settings.basic_memory_agent.faster_first_response` | — | 已固化为行为：回复逐句入队，首句可在逗号处提前断开（`conversation.Splitter`），无开关 |
| `agent_settings.basic_memory_agent.segment_method` | — | 同上，断句规则内置 |
| `agent_config.conversation_agent_choice` | — | Go 只实现 `basic_memory_agent` 一种循环 |
| — | `[agent].max_tool_rounds` | 新增：单轮对话内工具循环上限，默认 8 |
| — | `[agent].history_max_turns` / `history_max_tokens` | 新增：历史裁剪上限，默认 12 轮 / 4000 token |
| — | `[agent].queue_size` / `turn_timeout` | 新增：每会话待处理消息上限与单轮超时 |

### 2.3 TTS

`tts_config.tts_model`（单选一个引擎）变成 `[tts].engines`（**数组 = 降级顺序**）。

| conf.yaml | config.toml |
| --- | --- |
| `tts_config.edge_tts.*` | `[tts.edge_tts]`（`voice` 等） |
| `tts_config.openai_tts.*` | `[tts.openai_tts]`（`base_url` / `api_key` / `model` / `voice`） |
| `tts_config.siliconflow_tts.*` | `[tts.siliconflow_tts]`（`api_url` → `base_url`） |
| `tts_config.fish_api_tts.*` | `[tts.fish_api_tts]` |
| `tts_config.minimax_tts.*` | `[tts.minimax_tts]` |
| — | `[tts].engines = ["edge_tts", "openai_tts"]`（新增：按序降级） |

引擎输出统一为**裸 PCM16 24 kHz 单声道**（无容器头），采样率不一致时在包内重采样。

### 2.4 网关侧（原 config.toml 与 bridge config.json）

| 旧键 | config.toml | 说明 |
| --- | --- | --- |
| `[[clients]]`（网关） | 原样保留 | `platform` / `adapter_key` / `path` |
| `[memory].target` / `callback_platform` | 原样保留 | 事件终端已改为进程内会话，这两个键暂时不再生效（待清理） |
| `[memory].queue_size` / `queue_wait_timeout` / `sensitive_words_file` / `dedup_ttl` | 原样保留 | 上行管线的背压、敏感词、去重 |
| `listen_addr`（bridge） | `[server].addr` | 合并 |
| `llm.endpoint` / `model` / `api_key`（bridge） | `[llm].*` | 合并，意图分析不再单独存在 |
| `speech.cooldown_sec`（bridge） | `[broadcast].min_interval` | 冷却改为播报队列的全局间隔 |
| `speech.gift_bypass_cooldown`（bridge） | — | 优先级抢占已取代它：礼物/SC 的优先级本就高于弹幕 |
| `speech.reply_chance.*`（bridge） | — | 不移植：属"要不要说话"的策略层，见 §3 |
| — | `[broadcast].concurrency` / `max_pending` | 新增：并行合成上限与待合成条目上限 |

## 3. 明确删除的键面

| 旧键面 | 理由 |
| --- | --- |
| `asr_config.*` 全部 | **不做 ASR**：直播场景没有麦克风输入源，原保留是 Python 侧的遗留 |
| `vad_config.*` 全部 | VAD 只为 ASR 切句服务，随 ASR 一并剪除 |
| `tts_config.azure_tts` / `bark_tts` / `cosyvoice_tts` / `cosyvoice2_tts` / `melo_tts` / `x_tts` / `gpt_sovits_tts` / `coqui_tts` / `spark_tts` / `sherpa_onnx_tts` | 本地推理引擎全部不做（`sherpa` 需要 CGo，会破坏"单二进制"）；只保留 5 家云引擎 |
| `agent_settings.hume_ai_agent` / `letta_agent` | 第二套 Agent 实现，与主会话循环重复 |
| `agent_settings.basic_memory_agent.use_mcpp` | 不引入第二个循环，工具能力改为纯 Go 工具注册层（E7） |
| `system_config.tool_prompts.*` | 随 MCP 提示词模式一并取消 |
| `system_config.enable_proxy` | Python 侧靠内部代理把事件注入活跃会话；Go 版事件终端是进程内方法调用 |
| `tts_preprocessor_config.translator_config.*` | 翻译不作为播报链路的一环 |
| `tts_preprocessor_config.remove_special_char` 等 | 暂未实现；当前只做断句，需要时再补文本预处理 |
| `live_config.bilibili_live.sessdata` | B 站 web 协议（blivedm）通路已废弃；B 站事件由外部上报端经 `/bilibili` 上报（网关不做协议、不存凭据） |
| `character_config.human_name` | 未被任何实现消费 |

## 4. 凭据规则

- API key 一律**不进仓库**：`config.toml` 里留空，用环境变量覆盖
  `LLM_API_KEY` / `OPENAI_API_KEY` / `SILICONFLOW_API_KEY` / `FISH_API_KEY` / `MINIMAX_API_KEY`；
  `edge_tts` 不需要凭据。
- 角色文件只放人设与展示信息，不放凭据。
