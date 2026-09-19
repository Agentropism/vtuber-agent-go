# Go 重写执行计划(go-rewrite effort · 待审查)

> 关联 map:`docs/archive/go-rewrite/map.md`;状态:计划草案,待用户审查后转执行(阶段 E0–E8 的实际进度见 map 末尾)。
> 本文档是把 12 张决策票收敛为执行蓝图:第 1-2 节回顾已锁基线;第 3 节为剩余决策的推荐默认值(审查点);第 4 节为执行阶段;第 5 节验收与切换;第 6 节风险。

---

## 1. 已锁定的基线(grilling Q1–Q11 + tickets 01/02/03)

| 项 | 决议 |
|---|---|
| 范围 | 全 Go 化,收敛为单二进制;onebot-gateway 仓库内演进(旧三仓库冻结) |
| 契约 | 对外契约兼容:platformEvent 上传(20 字段)、/inject、B站开放平台线格式(11 类 cmd)、配置键;前端 /client-ws 协议自由重设计 |
| 前端 | 自研极简 HTML+Live2D;@jannchie/pixi-live2d-display 1.4.x + pixi.js 8 + 本地 cubismcore(frontend/libs/ 现成文件) |
| Go 栈 | gin + gorilla/websocket + go-openai(OpenAI 兼容);LLM 仅 OpenAI 兼容远程 |
| 引擎 | 只保留云 TTS 5(edge/openai/siliconflow/fish/minimax);删除全部本地推理/gpt_sovits/x_tts/Gradio/fun_asr;**ASR 整体不做**(直播场景没有麦克风输入源,属从 OLV 继承的遗留假设)、**sherpa 双链不做**(避免 CGo 构建链);不做音色克隆 —— **2026-09-16 用户确认** |
| 简化 | llm-vup-bridge 并入(不移植其 LLM 意图分析——双播报管线根除);删 blivedm;废弃 memory.py;删 Python live/ 层;mcpp 重建为 Go tool 注册层 |
| 切换 | 直接替换:Go 版 MVP 可用即停 Python |

## 2. 已 resolve 的决策票(01/02/03)对执行的关键约束

- **01** → E6 前端实现锚点:`model.speak()` 一条调用;JSON 信封;WAV PCM16 24kHz mono;前端 FIFO 播放队列;Go 端复刻 `extract_emotion` + `remove_emotion_keywords`;`/client-ws` 沿用路径。
- **02/03** → E4 引擎边界:引擎接口=「单句文本 → **裸 PCM16 24kHz 单声道字节**(不带容器头)」;failover 链按配置序;云引擎返回 mp3 时在包内解码并下混单声道。**2026-09-16 修订**:去掉 WAV 头(格式由包内常量约定);删去 sherpa 权重路径(该链不做)、删去 asr 相关约束。
- **08 记忆(2026-09-17 修订)**:原定 SQLite 存储聊天历史。CGo 已被排除,纯 Go 的 SQLite 驱动要拖进十几个模块(含上百 MB 的 libc 源码),而这里真正需要的只是「记住说过的话 + 按关键词找回来」。改为追加式 JSON Lines + 内存倒排索引(二元组词元),零依赖、可 grep,量级远未到需要索引结构。
- distillery 现状:**已删除(2026-09-16)**。原计划「保留转发能力用于 MVP 兼容」作废:礼物/SC/舰长本就经 `NeedsReply` 进会话由 agent 致谢并入队,转发是第二条旁路,同一礼物会被答谢两遍。冷却与优先级逻辑已在 E5 的 broadcast 包重建(`MinInterval`、`PriorityFor`);`/inject` 作为独立入口保留,但只做「文本+表情 → 入队」,不做 LLM 意图分析与模板话术。

## 3. 剩余决策票的推荐默认值(审查点 — 请逐条确认或修改)

| 票 | 推荐默认 | 影响 |
|---|---|---|
| 04 B站事件模型 | **直接复用现有 `internal/event/bilibililive` 包**(11 类 cmd、宽容化、Unknown 透传、open_id 身份规则、上传文本化格式全部维持),仅按需补新事件 | 零重设计,风险最低 |
| 05 会话编排 | 新建 `internal/conversation`:Session 管理器(按 channel_id 一对一,互不阻塞)+ 统一 `InboundEvent`(上传/inject/proxy 三类输入适配)+ 历史裁剪(近 N 轮 + token 上限,硬编码初值)+ 角色提示词(character yaml 转 TOML)。**工具调用 MVP 后补**,首批工具=状态查询 + 记忆检索 | 核心架构 |
| 06 播报优先级 | 新建 `internal/broadcast`:优先级 SC > 礼物 > 弹幕 > LLM 主动 > 待机;高优先级打断低优先(低优先重排队),同级 FIFO;`/inject` 与会话回复统一入队;待机发言最低级可随时打断 | 消灭双播报 |
| 07 前端协议 | **采用 01 报告第五节五条建议**:JSON 信封 `{type,seq,ts,payload}`;`audio` 原子绑定 {audio base64,wav, expression, reset_expression, display_text};上行回执 `audio_started/audio_finished/error`;模型清单走 HTTP(`/live2d-models/info` 语义保留);表情默认播完复位 neutral | 一次确认即闭环 |
| 08 记忆 | 简化版:**追加式 JSON Lines**(2026-09-17 修订,原定 SQLite)+ 关键词召回(无向量库);SC/礼物写入时权重标记并参与召回打分;范围=会话连续性与基础人设 | 不做复杂 RAG |
| 09 配置 | 单文件 `config.toml`(viper);键面=`[server] [log] [llm] [agent] [tts] [broadcast] [memory] [[clients]]`(源:conf.yaml 的 live/agent/tts/character/proactive + 原 config.toml;**asr/vad 键面随 ASR 一并删除**);凭据(API key)一律环境变量或独立 secrets 文件(gitignore),不进模板;`config_templates/` 同步更新 | 契约工程 |
| 11 仓库骨架 | 单 `go.mod`;包边界(实际落地,2026-09-16):`app/ gateway/{event,bilibili,server,upload,filter} agent/{conversation,conversation/llm,broadcast,memory,frontend,tool} tts/ shared/{action,event} config/ logger/` 入口 `cmd/onebot-gateway`;**无 asr 包**(ASR 不做)、无 CGo(**sherpa 不做**)、无 distillery 包(2026-09-16 删除,双播报管线合一);旧 Python 资产(characters/prompts/… )转 TOML/静态资源迁入 | 所有代码落点 |
| 12 B站客户端 | Go 重写等价 bilibili-live(`internal/bilibili` 库包):开放平台 WSS 线格式、签名、心跳、断线重连(5 次告警退出)、宽容化反序列化、Unknown 透传;bilibili-live 仓库冻结 | 接入层核心 |
| 10 MVP | MVP 范围=名单上线:①B站弹幕→会话→LLM→TTS→前端;②QQ 群消息接入+上传;③/inject 播报;④统一播报队列;⑤配置迁移;后补=记忆召回排序、工具调用、空闲发言/热点、SC 权重细化 | 切换判据 |

> 审查结论以「确认全部默认值」或逐条修改意见的形式返回;确认后本计划冻结为执行基线。

## 4. 执行阶段(顺序依赖,阶段内可并行)

### E0 仓库骨架与基线迁移(先于一切代码)
- 任务:go.mod 就绪;删减/整理 internal/ 现有包(保留 action/app/config/event/upload/filter/logger/server);建立新包空壳;契约 fixture 迁移:bilibili-live/tests/fixtures → Go 测试(同 JSON 断言 11 类 cmd 解析);CI 命令记录。
- 验证:`go build ./... && go vet ./... && go test ./...` 全绿;fixture 回归与 Rust 端等价。

### E1 配置与事件模型
- 任务:config_toml 单文件键面(09);platformEvent 与事件类型定义固化(04);下行 Action 契约段补充(上传回调)。
- 验证:配置样例 + 键面映射表文档;事件模型 fixture 测试。

### E2 接入层(B站 + QQ + 上传)
- 任务:internal/bilibili Go WSS 客户端(12);QQ OneBot 接入沿用现 server/event 链;上传管线(现有 upload 包)接通;下行 Action 转发打通(现 forwardAction → server.SendAction)。
- 验证:mock 开放平台帧进网关 → platformEvent 正确上传;下行 Action 回写 mock 客户端;无凭据可跑的单元测试。

### E3 会话核心
- 任务:internal/conversation(Session 管理器、InboundEvent 适配器、历史裁剪、角色加载);LLM 客户端(go-openai,OpenAI 兼容 base_url,流式);注入路径:/proxy-ws 收 platformEvent → 会话;聊天历史持久化(为 E5 记忆留接口)。
- 验证:三类输入注入同一会话;流式回复 → 句子分段 → 逐句投递(顺序不乱);无活跃前端不丢事件(队列)。

### E4 语音引擎 —— ✅ 已完成(2026-09-16)
- 任务:`tts/`(引擎接口 + 5 家云引擎 + failover 链);统一输出**裸 PCM16 24kHz 单声道**(云引擎返回 mp3 时在包内解码并下混单声道;采样率不符时重采样)。
- **2026-09-16 修订**:ASR 整体不做(`asr/` 包取消)、sherpa 双链不做(不引入 CGo),因此本阶段**无 CGo 构建链问题**。
- 验证:每引擎 httptest 假端点单测(请求路径/鉴权/请求体字段)、mp3 解码下混单声道回归测试、非契约采样率重采样、failover 顺序 —— 21 个用例全绿。

### E5 播报队列
- 任务:internal/broadcast(优先级、打断/重排队、冷却);/inject 接入(与旧协议 100% 兼容现实:POST {text, emotion, intensity} → 入队);TTS 任务管理(并行合成/按序投递,对应 Python TTSTaskManager)。
- 验证:优先级场景集成测试(SC 打断弹幕回复、待机可随时打断)。

### E6 前端
- 任务:frontend/(index.html + app.js + vendored pixi/pixi-live2d-display/cubismcore);/client-ws 新协议实现(01 五条);/live2d-models/info + 模型静态托管;/client-ws 与 /proxy-ws 并存。
- 验证:浏览器连 Go 后端,mao_pro 模型说话口型+表情切换+字幕;shizuku 无表情不报错;iOS 手势 resume 备注。

### E7 记忆与工具(MVP 后补项,排在 E6 后)
- 任务:internal/memory(SQLite 历史+关键词召回+权重);tool 注册层(状态查询/记忆检索);空闲发言/热点搜索若列入 MVP 则在此。
- 验证:召回单测;SC 权重排序生效。

### E8 MVP 集成与切换
- 任务:全链路联调(与真实 B站开放平台 + QQ 客户端);配置迁移指南;旧系统并行观察;切换判定(10 清单全绿)→ Python 停用;回滚预案(保留 Python 启动命令,回退开关=配置切换)。
- 验证:用户实播验收;切换后 24h 观察日志与事件链路。

## 5. 里程碑

| 里程碑 | 内容 | 依赖 |
|---|---|---|
| M1 | E0+E1 完成:骨架绿、契约 fixture 通过、配置键面冻结 | 审查批准 |
| M2 | E2 完成:双平台接入 + 上传 + 下行接通 | M1 |
| M3 | E3+E4 完成:弹幕→LLM→TTS 链路可跑(/inject 亦可) | M2 |
| M4 | E5+E6 完成:播报优先级 + 前端 Live2D 全链路 | M3 |
| M5 | E7+E8 完成:MVP 验收与切换 | M4 |

## 6. 风险

| 风险 | 等级 | 缓解 |
|---|---|---|
| ~~CGo(sherpa binding)构建链/平台兼容问题~~ | **已消除** | 2026-09-16 决定:ASR 不做、sherpa 不做,全链路无 CGo,单二进制纯 Go |
| 云引擎地区封锁/配额(edge 等) | 中 | failover 链;配置可换引擎 |
| /client-ws 新协议与模型交互细节 | 中 | 01 报告已含坑清单;E6 先 protototype 再集成 |
| 事件契约漂移(三语言→单语言后风险大降) | 低 | fixture 共享测试 |
| 切换期双系统维护 | 低 | 直接替换策略已定,观察窗口 24h |

## 7. 实施状态(2026-09-16)

| 阶段 | 状态 | 剩余 |
|---|---|---|
| E0 骨架与基线 | ✅ | — |
| E1 配置与事件模型 | 🟡 | 配置键面映射表文档(conf.yaml → config.toml);「下行 Action 契约段」随 upload 的 WS 移除失去对象,待删或重新定义 |
| E2 接入层 | ✅ | 内置 `gateway/bilibili` 开放平台长连接(签名/帧/双心跳/重连),修掉原 Rust 四处实证缺陷 |
| E3 会话核心 | ✅ | 句子分段、历史裁剪(12 轮/4000 token)、角色资产(`characters/*.toml`) |
| E4 语音引擎 | ✅ | — |
| E5 播报队列 | ✅ | — (`/inject` 端点已接入统一队列;`gateway/distillery` 已删除,见 §2) |
| E6 前端 | ✅ | 自研页面(go:embed)、`/client-ws`、`/live2d-models/info`、`shared/emotion` 复刻标签解析、Web Audio 口型同步 |
| E7 记忆与工具 | ✅ | **记忆改为追加式 JSON Lines**(见 §2 修订)、关键词二元组召回 + 权重、`agent/tool` 注册层(memory_search/bot_status)、主动发言;热点搜索仍后补 |
| E8 集成与切换 | 🟡 | 配置迁移 `docs/CONFIG_MIGRATION.md`、切换与回滚 `docs/CUTOVER.md` 已就绪;真实账号联调待执行 |

已可运行的部分:弹幕/群消息 → 会话 → LLM → 回复 → 播报队列 → TTS → Sink(占位 logSink),
已用「真二进制 + 假 LLM + 假 TTS + 真 WS 客户端」做过端到端冒烟验证。
`/inject` 与事件回复共用同一条队列(契约见 `onebot-gateway/docs/INJECT_API.md`)。
`[tts].engines` 留空时整条播报链路不装配,只做文本下行,`/inject` 返回 503。

## 8. 审查确认单

- [ ] 第 3 节十个推荐默认值(04-12)确认或修改
- [ ] E8 切换判据(MVP 清单)确认
- [ ] 阶段顺序与里程碑(M1-M5)确认
- [ ] 其他增删