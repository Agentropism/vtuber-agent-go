# SyAgent 全栈架构重构计划(2026-09-06)

> 状态:规划中(待批准)
> 目标仓库:Open-LLM-VTuber(Python) / onebot-gateway(Go) / bilibili-live(Rust) / llm-vup-bridge(Go,遗留)
> 原则:小步可验证、契约先行、每阶段可回滚、中文注释与日志、按任务聚焦提交


> **已归档并失效(2026-09-19)**:本文描述的三仓库(Open-LLM-VTuber / onebot-gateway / bilibili-live + llm-vup-bridge)
> 协作架构已被「单二进制 Go 重写」取代:Go 版承载全部职责,Python 项目移出仓库,`docs/PRD.md` 删除。
> 下文对 PRD 需求与 `docs/PRD.md` 的引用只作历史溯源用。
---

## 0. 现状盘点(证据清单)

### 0.1 当前拓扑

```
QQ/OneBot 客户端 ──WS──▶ onebot-gateway(:6199)
                           ├─ /qq        handler → upload ──WS──▶ Open-LLM-VTuber(:12393)/proxy-ws
                           │                              (platformEvent JSON = 记忆服务入口, 即它自己)
                           ├─ /bilibili  handler → upload ────────┘ │
                           │                    │                  └─▶ ProxyHandler → 注入活跃浏览器 session
                           └─ 高价值事件 ─HTTP─▶ distillery(:9528, llm-vup-bridge)
                                                            └─ LLM 意图分析 → POST /inject
                                                              → TTS+口型+表情广播(第二条播报管线)

B站开放平台 WSS ──▶ bilibili-live(Rust) ──WS──▶ onebot-gateway /bilibili        (通路 A)
B站 web 协议(blivedm + SESSDATA) ──▶ Open-LLM-VTuber 内嵌 ──proxy-ws──▶ 直接注入  (通路 B, 并行的第二条 B站通路)

前端浏览器 ──WS──▶ Open-LLM-VTuber /client-ws (音频帧 + 表情 + 字幕)
```

### 0.2 关键事实

| # | 事实 | 证据 |
|---|------|------|
| F1 | Open-LLM-VTuber 身兼三职:VTuber 交互核心(:12393)、记忆服务入口(`/proxy-ws` 收 `platformEvent`)、会话注入端 | `onebot-gateway/config.toml` `[memory].target = ws://127.0.0.1:12393/proxy-ws`;`proxy_handler.py` 注释「接收 onebot-gateway 的 upload 事件,直接注入到 WebSocketHandler 的活跃浏览器客户端 session」 |
| F2 | 两条 B 站事件通路并存:A=Rust 开放平台(bilibili-live→gateway→记忆+语音反馈);B=Python blivedm web 协议(→proxy-ws 直注入)。事件结构、用户身份(open_id vs uid)、覆盖范围均不一致,同一直播间事件可能被两套处理 | `bilibili_live.py` L303 `proxy_url = "ws://localhost:12393/proxy-ws"`;`bilibili-live/src/bin/bin.rs`(开放平台→CONNECT) |
| F3 | 两套播报管线并存:会话注入(proxy-ws→session→LLM→TTS)与 /inject 播报(distillery→广播)。高价值事件(礼物/SC/舰长)经 distillery 用 **独立于主会话** 的 LLM 分析生成感谢词,与主会话互不知晓 | `llm-vup-bridge/README.md`;`onebot-gateway/internal/upload/client.go` 转发 distillery |
| F4 | 下行 Action 回调是半成品:`upload/client.go` 已实现 `handleCallbackAction→forwardAction` 并注入转发,但 `doc/UPLOAD_API.md` 明说「记忆服务不应依赖下行回调驱动业务」——契约与实现矛盾 | `upload/client.go` L239-255;`Open-LLM-VTuber/doc/UPLOAD_API.md` §6 |
| F5 | 记忆双轨:网关上传的「记忆服务」= Open-LLM-VTuber 的 proxy-ws;同时 `memory.py` 有本地 MemoryStore(SQLite 类)。PRD「SC 高权重记忆」尚未落地 | `memory.py` `MemoryStore`;`docs/PRD.md` 需求九 |
| F6 | 三套配置并存:Python `conf.yaml`(YAML,24KB)、网关 `config.toml`(TOML)、distillery `config.json`(JSON)。端口/密钥/引擎均分散;属性无跨项目共享 schema | 各项目仓库根 |
| F7 | 契约文档归属混乱:网关协议 `UPLOAD_API.md` 同时存在于 `onebot-gateway/docs/` 与 `Open-LLM-VTuber/doc/`,同源异构易漂移 | 两处同名文件 |
| F8 | llm-vup-bridge 是遗留态:2026-07-27 上传后仅 README 更新;其能力(distillery 意图分析、gift 分档)与网关功能重叠 | git log;`internal/dispatch/` |
| F9 | 架构愿景已定(Open-LLM-VTuber AGENTS.md):Python 保留语音模块;接入层用 Go 传 WS;VUP 渲染层将用 Rust 重构;考虑集群/多 Agent/RAG/高并发 | `Open-LLM-VTuber/AGENTS.md` |
| F10 | B 站事件 struct 未全局共享:开放平台事件在 bilibili-live(Rust)、onebot-gateway(bilibililive 包,Go)、上传文本(Go)三处各有定义,字段漂移靠手写兼容器兜底 | `bilibili-live/src/de.rs` int/string 兼容器;`onebot-gateway/internal/event/bilibililive/` |

### 0.3 问题清单(按优先级)

1. **双 B 站通路**(F2)——资源浪费、事件重复、行为不一致(Python 直连还会被 SESSDATA/B 站 web 协议风控影响)。
2. **双播报管线**(F3)——礼物/SC 感谢词与主会话割裂,PRD 四(优先级调度)、八(消息分级)无法在不重构前提下落地。
3. **三合一职责耦合**(F1)——Open-LLM-VTuber 是"交互核心+记忆服务+注入端",任何一部分的重启/扩容都会互相拖累,违背集群/高并发愿景。
4. **契约无单一事实源**(F6/F7/F10)——事件 JSON、配置键、端口全靠三个仓库心照不宣。
5. **下行回调半成品**(F4)——要么接通(记忆服务→网关→平台 Action),要么删掉,不能留"文档说不用、代码却实现了"的矛盾。
6. **llm-vup-bridge 定位未决**(F8)——继续演进还是并入网关。

---

## 1. 目标架构

### 1.1 战略目标(对齐 AGENTS.md 愿景 + PRD)

- **T1 单一接入面**:所有平台(QQ/B站/未来)事件经 onebot-gateway 单一入口;bilibili-live 只做开放平台协议解码,输出标准化事件。
- **T2 明确服务边界**:Open-LLM-VTuber 收敛为「对话+语音引擎」;记忆/注入/播报职责拆到清晰接口之外,可独立演进与扩容。
- **T3 契约单一事实源**:一个跨项目事件 schema + 一份共享配置模板,三语言按契约各自实现,CI 校验契约一致性。
- **T4 播报管线合一**:高价值事件反馈收敛为「网关→(记忆/会话)→统一播报队列」单一路径;distillery 的意图分析价值保留但不再是旁路。
- **T5 为 Rust VUP 重建与集群扩展留接口**:PRD 十(VUP 解耦)与八/九(消息分级、SC 高权重记忆、Tool Calling)作为本计划尾部阶段,而不是下一次重构。

### 1.2 目标拓扑(Phase 5 完成后)

```
QQ/OneBot ──▶ onebot-gateway ──┬──▶ 记忆服务(独立进程或 Open-LLM-VTuber 的隔离路由)    [T2/T3]
B站(开放平台) ──▶ bilibili-live ──▶ onebot-gateway ──┴──▶ 统一事件总线/队列 ──▶ Open-LLM-VTuber 会话核心
                                        │                       │
                                        └── Action 下行 ────────┘(接通,非半成品)         [T1/T4]
Open-LLM-VTuber 会话核心 ──▶ 播报队列(优先级调度) ──▶ TTS/Live2D 驱动接口(VUP Rust 侧)    [T4/T5]
```

---

## 2. 分阶段实施计划

> 每阶段:目标 → 任务(含涉及仓库)→ 验收 → 风险。阶段间有顺序依赖,阶段内可并行。
> 全程纪律:每个任务一个聚焦 commit;`ruff check` / `go vet`+`go build`+`go test` / `cargo clippy -D warnings` 按仓库最小充分验证;契约改动先改文档再改代码。

### Phase 0 — 契约冻结与基线(预计 1 周)

**目标**:在动手改任何代码前,把事件契约、配置面、端口/路径固化成文档,建立跨仓库一致性测试。

任务:

1. **发布《事件契约 v1》**(docs/ 新建 `event-contract-v1.md`):
   - 冻结 `platformEvent`(网关→记忆服务上行,现有 20 字段)为唯一公共事件结构;
   - 冻结 bilibili-live 开放平台事件 → 网关内部事件的映射(11 类 cmd);
   - 明确 `type: message|notice` 语义、`is_tome/is_self/ref_*` 字段的去留(现状多数字段恒空,按 YAGNI 收编)。
2. **发布《配置契约 v1》**:三套配置的公共键对齐表(端口、memory.target、distillery、SESSDATA/凭据位置),规划单模板来源(`config_templates/` 与 `config.toml.example` 同源或共享生成)。
3. **建立契约 CI 检查**:三个仓库各加一个 fixture 解析测试,把同一组事件 JSON fixture 各自解析并断言字段等价;`bilibili-live/tests/` 已有 18 个 fixture 可复用为种子。
4. **基线记录**:记录每仓库当前构建/测试状态、已知 TODO/FIXME,写入交付基线文档。

验收:

- 事件契约文档无「当前恒空/未实现」标记,字段均有明确语义;
- 三仓库 CI 均能在本地对同一 fixture 集通过;
- 无任何代码行为变化(纯文档+测试)。

风险:低。主要成本是文档与测试工时。

### Phase 1 — 接入面收敛(预计 2 周)

**目标**:消灭第二条 B 站通路(Python blivedm),让 onebot-gateway 成为唯一事件入口(T1)。

任务:

1. **B 站通路迁移(核心)**:
   - 将 `bilibili_live.py` 依赖的弹幕/礼物/SC/大航海 onClicked 数据源全部改为读取网关事件;若 VOCAL 需要,Open-LLM-VTuber 从自身 proxy-ws 或新增内部 feed 端点消费;
   - 验证 bilibili-live 开放平台在真实房号的可用性(PRD 需求一/二已覆盖自动重连与事件补全),确认可替代 blivedm 后删除：
     - `Open-LLM-VTuber/src/open_llm_vtuber/live/bilibili_live.py` 中 blivedm 分支;
     - `conf.yaml` 中 blivedm 相关键(room_ids/sessdata 若仅用于此通路);
     - 相关文档 `doc/UPLOAD_API.md` 的 blivedm 引用。
   - 保留 `live/live_interface.py` 抽象,未来新平台(抖音等)走同一接口挂到网关侧,不在 Python 里再起协议客户端。
2. **网关 B 站侧补齐**:对照 blivedm 覆盖范围检查网关 `event/bilibililive/` 缺失事件(B站 web 协议独有的高能/盲盒等不在开放平台的,列入「不迁移」白名单文档)。
3. **用户身份对齐**:统一 open_id 优先、uid 兜底(现有 `bilibiliUserID` 逻辑)为唯一身份规则,写进契约 v1。

验收:

- 全链路(真实或 mock 开放平台事件)→ 网关 → Open-LLM-VTuber 会话注入,行为与改造前 blivedm 一致(弹幕进会话、礼物/SC 有反馈);
- `bilibili_live.py` 中 blivedm 代码删除,`BLIVEDM_AVAILABLE` 分支移除;
- 无 SESSDATA 依赖的运行路径可完整跑通(风控面收窄)。

风险:中。开放平台与 web 协议事件覆盖面有差异(白名单中列明的场景需用户接受或另期补齐)。

### Phase 2 — 服务边界解耦(Open-LLM-VTuber)(预计 2-3 周)

**目标**:把 F1 的三合一拆成清晰边界,让「交互核心」可独立于「记忆/注入」演进(T2)。

任务:

1. **会话核心与 IO 分层**:
   - `websocket_handler.py` 拆为:会话编排(core)+ WS 传输(io)+ 注入适配(input port);`proxy_handler.py` 从「注入到活跃浏览器连接」改为「注入到会话输入队列」,消除「无活跃浏览器就丢事件」的耦合(`proxy_handler.py` L69 `inject_upload_message` 依赖活跃 client);
   - 定义内部 `InboundEvent` 接口,`UploadEvent`(网关 payload)是第一个实现,`/inject` 事件是第二个,未来平台事件是第三个。
2. **记忆服务入口独立化**:
   - 决策点:记忆存储继续内嵌(`memory.py` MemoryStore)但作为独立路由/模块,网关上传与 LLM 记忆召回共用同一 store 接口;或拆出独立记忆进程(若集群化先行,该进程先行)。
   - 无论哪种,`/proxy-ws` 的协议语义从「VTuber 私有注入」升级为「公开记忆+事件入口」,写回契约 v1。
3. **配置面收敛**(配合 Phase 4):`conf.yaml` 中的 `live/`、`server.proxy` 段与网关 `config.toml` 的 `[memory]` 段建立映射文档,消除「为什么 12393 既是 app 又是记忆」的隐式约定。

验收:

- 无活跃浏览器客户端时,收到的事件仍进入会话队列(不丢);重连后可见(此为本阶段验收的核心行为变化);
- 网关上传、`/inject`、proxy 注入三类输入走同一 `InboundEvent` 适配器,单测覆盖;
- 记忆 store 单元测试与网关 fixture 联测通过。

风险:高(触碰核心)。缓解:Phase 0 契约先行 + 每步保留旧代码路径开关(`memory.py` 默认关闭的先例可沿用)。

### Phase 3 — 播报管线合一 + 下行回调接通(预计 2 周)

**目标**:T4——消灭双播报;F4——让下行 Action 从半成品变为可用或彻底移除。

任务:

1. **决策点 A:distillery 的去留**(需用户拍板,见 §4):
   - 方案 3a:并入网关——`distillery` 的意图分析逻辑(gift 分档、冷却调度)迁入 onebot-gateway,事件处理链内直接产出播报请求;llm-vup-bridge 仓库归档。
   - 方案 3b:保留独立 distillery 服务,但事件源唯一化(只从网关收),输出仍走 `/inject`——两套管线在「播报」层合并为一条队列。
   - 推荐 3a:播报逻辑 = 状态机(优先级/冷却),归网关;LLM 意图分析 = 已有 MCP/Tool 架构(Open-LLM-VTuber `mcpp/` 已存在),成长为统一 Skill 层,避免第二个"半套 Agent"。
   - **决议(2026-09-16)**:取 3a 并更进一步——`distillery` 整包删除。礼物/SC/舰长本就经 `NeedsReply` 进会话由 agent 致谢、再按 `PriorityFor` 入播报队列,转发是第二条旁路(同一礼物被答谢两遍);意图分析不再单独存在(agent 自身即 LLM),礼物模板话术、冷却调度分别由 agent 与 `broadcast.MinInterval` 取代。`/inject` 作为独立入口保留,只做「文本+表情 → 入队」,契约见 `onebot-gateway/docs/INJECT_API.md`。
2. **统一播报队列**:Open-LLM-VTuber 新增 `conversations/priority_queue.py`(PRD 四:SC>礼物>普通弹幕>待机),`/inject` 与会话回复统一入队;删除 `llm-vup-bridge` Python 侧的并发限制注释逻辑(README「已知限制」逐条关闭)。
3. **下行 Action 接通**:在网关 `readLoop→handleCallbackAction` 基础上,把 Action 分派到对应平台的 `server.SendAction`;契约 v1 增补下行段;若 Phase 3 内不能接通,则删除转发代码(二选一,不留悬空)。

验收:

- 一条链路上:事件 → 网关 →(记忆/播报队列)→ TTS → 前端,无第二旁路;双播报日志消失;
- 反向:记忆服务/上游下发的 Action 能回到 QQ/B站客户端(或用 mock 网关验证);
- `llm-vup-bridge` 仓库达到归档或并入状态,README 与实现一致。

风险:中高。方案 3a 涉及 Go 侧新增状态机与测试;方案 3b 风险更低但保留了双进程运维面。

### Phase 4 — 配置与部署统一(预计 1-2 周)

**目标**:F6/F7——一套配置心智 + 一键部署。

任务:

1. **配置收敛**:三个仓库公共配置(端口、target、凭据路径)合并到单一来源模板;各语言仍各读各的格式,但由同一模板生成/校验(简单方案:一个 `config/` 目录 + generator 脚本;激进方案:统一 TOML)。
2. **一键部署**:`docker-compose.yml`(工作区根)编排 Rust 客户端、Go 网关、Python 核心、记忆进程(如独立化);健康检查与重启策略落位(PRD 一自动重连在进程级闭环)。
3. **文档收编**:契约文档统一放 `docs/`,删除 `Open-LLM-VTuber/doc/UPLOAD_API.md` 与 `onebot-gateway/docs/UPLOAD_API.md` 的重复,指向唯一源。

验收:

- 新环境 `docker compose up` 五分钟内拉起全链路并可注入一条事件;
- 三份配置均从同一模板生成,CI 校验一致性;私有凭据不进模板、不进 git。

风险:低。主要成本在编排与文档。

### Phase 5 — 扩展点落地(预计 3-4 周,可与 Phase 2/3 部分并行)

**目标**:T5——把 PRD 八/九/十变成架构上的扩展开关,而非下一次"巨大重构"。

任务:

1. **消息分级(L1/L2/L3)**:在网关侧完成 L1 关键词直答(不消费 LLM);L2 标准会话(复用 Phase 2 的 InboundEvent);L3 人工介入(使用 Phase 3 接通的下行 Action)。PRD 八。
2. **SC 高权重记忆与 Tool Calling**:`memory.py` 权重写入(SC>弹幕>通知)+ 召回加权;LLM Tool Calling 检索记忆/触发动作。PRD 九。`mcpp/` 已有 MCP 骨架,注册为工具而非再起旁路。
3. **VUP 渲染层 Rust 化接口**:定义 `Live2DDriver` 接口(口型 sync / 表情 / 动作 / 优先级),Python 侧只发驱动指令,渲染层可替换(现状 Live2D 前端 → Rust 渲染);与 Phase 3 播报队列对接。PRD 十的第一阶段(解耦),不算全部完成 PRD 十。

验收:

- L1 消息响应延迟 < 100ms(不经过 LLM);
- SC 事件在记忆召回中权重生效(单测);
- 替换渲染层为模拟驱动时,会话零改动通过集成测试。

风险:高(横向跨三仓库)。阶段内任务相互独立,可分批交付。

---

## 3. 依赖关系与里程碑

```
Phase 0 ─▶ Phase 1 ─▶ Phase 2 ─▶ Phase 3 ─▶ Phase 4
   │          │          │           │          │
   └──────────┴──── Phase 5 ─────────┴──────────┘(2/3 完成后即可部分并行)
```

| 里程碑 | 标志 | 预计 |
|--------|------|------|
| M0 | 契约 v1 冻结、三仓库 CI fixture 通过 | 第 1 周 |
| M1 | 单 B 站通路(blivedm 删除,行为等价) | 第 3 周 |
| M2 | Open-LLM-VTuber 输入边界清晰(三类输入同一适配器、无活跃连接不丢事件) | 第 5-6 周 |
| M3 | 单播报管线 + 下行 Action 决策落地 | 第 7-8 周 |
| M4 | 一键部署 + 单配置来源 | 第 9-10 周 |
| M5 | L1/L2/L3 与 SC 记忆权重,渲染接口可用 | 第 12-14 周 |

## 4. 需用户拍板的决策点

1. **D1(Phase 3)distillery 并入网关(3a)还是保留独立服务(3b)?** 推荐 3a,减少第二个 Agent 半实现。
2. **D2(Phase 2)记忆服务内嵌(模块化)还是独立进程?** 若短期只做单体部署,先内嵌+接口化;若集群化先行,优先独立。
3. **D3(Phase 1)B 站 web 协议独有事件(开放平台没有的)是否接受白名单缺失?** 影响 live 功能完整性 vs 风控面。
4. **D4(全局)三语言契约的 CI 一致性验证工具**:先用仓库内 fixture 测试(低成本),还是引入独立 schema 工具链(如 buf / asyncapi)?
5. **D5(Phase 4)配置模板策略**:生成器脚本(简单)vs 全部收敛为 TOML(彻底)。

## 5. 风险登记

| 风险 | 等级 | 缓解 |
|------|------|------|
| Phase 2 触碰会话核心导致线上回归 | 高 | Phase 0 测试先行;旧行为保留开关 |
| 开放平台事件覆盖不足 | 中 | 白名单文档 + 用户验收 |
| distillery 并入网关工作量低估 | 中 | 3b 为降级方案,不阻塞其他阶段 |
| 三仓库并行地狱(契约漂移) | 中 | 契约 v1 冻结 + fixture 测试强制同步 |
| Rust 渲染层重构外溢 | 中 | Phase 5 只做接口,渲染实现另行立项 |

## 6. 执行方式建议

- 每阶段一个独立分支,阶段完成合入并打 tag(`refactor/phase-N`);
- 契约改动(事件字段、配置键)先更文档 → 三仓库各自实现 → CI 验证 → 删旧路径;
- 每阶段交付说明沿用 `docs/delivery-*.md` 格式,注明验证命令与未验证的外部依赖;
- 优先保证「直播中的即时反应」与「单次响应速度」不被任何阶段回退(PRD 立项初衷)。