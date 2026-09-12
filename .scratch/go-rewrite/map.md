# Go 重写:整个 SyAgent 项目 Go 化单二进制

## Destination

用 Go 在 onebot-gateway 仓库内把整个 SyAgent 系统重写为**单个高性能 Go 二进制**:接入(B站开放平台 WSS + QQ OneBot WS)+ 事件分发 + 记忆上传 + 会话/LLM 编排 + TTS/ASR(仅远程 API)+ 统一播报队列 + 极简 HTML+Live2D 自研前端托管。对外契约(platformEvent 上传、/inject 播报、B站开放平台线格式 11 类 cmd、配置键)保持兼容;前端协议自由重设计。简化:删除 Python blivedm 通路、llm-vup-bridge(并入网关)、全部本地推理引擎、Python live/ 层、旧 memory.py 实现。LLM 仅 OpenAI 兼容远程协议。切换:直接替换,Go 版最小可用即停 Python。

## Notes

领域:实时语音交互 VTuber(事件 → 会话 → TTS → 前端),直播即时反应优先。
技能:grilling / domain-modeling 必用,research / prototype 按 ticket 类型使用。
强制约定:中文注释与日志;Go 主流栈(gin + gorilla/websocket + go-openai 等);契约改动先改文档再改代码;keep focused commits。
仓库策略:onebot-gateway 内演进(grilling Q9),旧三仓库冻结作参考;bilibili-live 协议能力 Go 化。
Effort 组织:见 docs/agents/issue-tracker.md 的 Wayfinding operations(Type/Status/Blocked by 行;frontier = 开放且未阻塞且未认领)。

## 已确认基线(grilling 2026-09-06,明细将落到各 ticket)

- Q1 范围:全 Go 化,收敛单二进制
- Q2 上轮六阶段重构计划作废,继承其契约冻结思想
- Q3/Q6 契约:对外契约(上传/inject/B站线格式/配置键)全兼容;前端 /client-ws 协议自由
- Q4/Q7 前端:自研极简 HTML+Live2D,复用 live2d-models 资产
- Q5 简化:llm-vup-bridge 并入、删 blivedm、废弃 memory.py、TTS/ASR 长尾裁剪(远程 API 类保留)、删 Python live/ 层、mcpp 重建为 tool 注册层
- Q8 Go 栈:主流框架(gin/gorilla/go-openai)
- Q9 仓库:onebot-gateway 仓库内演进
- Q10 LLM:仅 OpenAI 兼容协议(无本地推理)
- Q11 切换:直接替换

## Decisions so far

- 2026-09-06 grilling 基线(Q1–Q11,见上)——本 map 的起点;各决策明细由对应 ticket 决议后回填索引
- [02 TTS/ASR 引擎盘点](issues/02-tts-asr-engine-survey.md) — 当前启用全为本地 sherpa 双链(Go 需替代);保留候选=云 TTS 5(edge/openai/siliconflow/fish/minimax)+ 本地 HTTP 2(gpt_sovits/x_tts)+ 云 ASR 2(groq/azure);报告见 artifacts/02-tts-asr-engine-survey.md
- [01 前端 Live2D 渲染栈](issues/01-frontend-live2d-stack.md) — 资产全 Cubism 3(mao_pro + shizuku);推荐 @jannchie/pixi-live2d-display 1.4.x + pixi 8 + 本地 cubismcore;model.speak() 一条调用驱动口型+表情;协议建议 5 条见 artifacts/01-frontend-live2d-stack.md
- [03 TTS/ASR 保留清单与 sherpa 替代](issues/03-tts-asr-retention-confirm.md) — 保留云 TTS 5 + 云 ASR 2;删本地 HTTP/Gradio/全部本地推理;shherpa 双链走官方 Go binding(CGo,复用 models/ 权重);不做音色克隆;砍 fun_asr
- [04–12 执行计划 §3 批准](execution-plan.md) — 用户审查通过执行计划,十项默认值全确认(事件模型复用现有包、会话编排 E3、播报优先级 E5、前端协议采用 01 五条、SQLite 记忆、单 TOML 配置、骨架 16 包、B站客户端 Go 化、MVP 五件套);执行阶段 E0–E8、里程碑 M1–M5 冻结

## Not yet specified

- 记忆新方案的具体检索与权重(SC 权重,PRD 九第一块)——等 08 决议
- 工具调用首批工具集与 mcpp 重建范围细节——等 05 决议
- 前端页面细节(设置面板、音量、模式切换)——等 01/07 决议
- 性能目标与压测方案——MVP 之后
- 部署形态(systemd/docker)——MVP 之后
- 日志与可观测(结构化日志、指标)——MVP 之后

## Out of scope

- 集群 / 多实例 / 多 Agent 编排(未来另行立项)
- 任何本地推理(LLM/TTS/ASR 一律远程)
- llm-vup-bridge 独立运行(并入网关,仓库冻结)
- Python 端任何模块的保留或维护
- blivedm / B站 web 协议通路
- 旧前端 submodule(Open-LLM-VTuber-Web)的维护
- Rust bilibili-live 仓库继续演进(协议能力 Go 化后冻结)

## 进度：30%

下一步：执行阶段推进——E0 已完成(骨架+fixtures 契约测试+类型漂移修复)并提交;下一步 E1 配置契约(config.toml 单文件键面 + 事件模型固化 + 下行 Action 契约段),完成 M1。