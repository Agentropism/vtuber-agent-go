# Grill Me Results

Generated: 2026-09-21T02:46:44.031Z

## Plan

修改代码的架构，改为前端-后端模式，后端暴露常用的交互和配置接口给前端，修改目录结构，将核心的内容封装为core交给backend使用

## Shared Understanding

计划「改为前端-后端模式：后端暴露交互与配置接口给前端，核心内容封装为 core 交给 backend 使用」经两轮访谈 + 一轮范围确认收敛为 23 条决议，无未决项。

**目标形态**：单 Go module 内分三层。`internal/core/` = 业务与领域能力（shared/config/logger/agent/tts/stream + 事件解析与上传管线 gateway/{event,upload,filter}），硬判据是**不 import net/http 与 websocket**；`internal/backend/` = 传输与接入（server 的 WS 接入与 Action 写回、api 的 REST 接口、web 的前端资源托管与 embed 产物、app 装配）；顶层 `frontend/` = 同仓库前端工程（本轮只搬迁现有原生 JS 页面，UI 一行不改），源码经 `go:generate` 拷进 `internal/backend/web/assets/`（gitignore 产物）后被 go:embed，部署仍是单个可执行文件。

**接口面**：REST(`/api/*`) 负责请求-响应，`/api/client-ws` 仍只做推送与回执；破坏式改名且旧路径不留别名——`/inject`→`/api/speak`、`/client-ws`→`/api/client-ws`、`/web/`→`/`（页面占根）、`/live2d-models/`→`/api/models/`、`/login/*` 保持限回环、`[[clients]].path` 是配置项不在改名范围。第一批四项接口：`GET /api/config`（只读 + 密钥脱敏为「是否已配置」）、`/api/speak`、会话列表/历史/以观众身份发消息、`GET /api/status`；第二批（记忆写、模型切换与表情、TTS 试听、推流开播/关播、工具裸调）留下一轮。

**纪律**：搬目录（零行为变化）→ 加 API（旧路径仍可达）→ 一次性路径切换（旧路径下线 + 页面路径常量 + 推流渲染地址同批），每步一个聚焦提交、每步 gofmt+build+vet+test 全绿 + e2e 冒烟（并为新 API 增加 HTTP 断言步骤）；core/backend 单向依赖靠 AGENTS.md 明文 + 一条 import 边界测试强制；AGENTS.md、README、既有契约文档与新增 API 契约文档同批更新。鉴权经明确确认后保持「全部开放、不鉴权」。

## Questions and Answers

### 1. 「前端-后端模式」里的「前端」指哪种形态？

**Recommended answer:** 同仓库前端工程 + 产物 embed（推荐）

**User answer:** 同仓库前端工程 + 产物 embed（推荐）

**Status:** resolved

**Notes:** 前端升级为仓库内独立工程目录，构建产物仍 embed 回二进制；保住「部署只有一个可执行文件」的不变量。

### 2. 页面技术栈怎么定？

**Recommended answer:** 原生 JS + 后端驱动通用表单（推荐）

**User answer:** 本轮不动页面，只出 API

**Status:** resolved

**Notes:** 页面维持现状（原生 JS、无构建），本轮后端只出接口；与「同仓库前端工程 + embed」「迁移第三步改前端」的时序需在后续轮次澄清。

### 3. core 的边界画在哪？

**Recommended answer:** core = 业务与领域能力，backend = 传输+接入+装配（推荐）

**User answer:** core = 业务与领域能力，backend = 传输+接入+装配（推荐）

**Status:** resolved

**Notes:** 判据：core 不 import net/http 与 websocket。core: shared/config/logger/agent/tts/stream + 事件解析与上传管线；backend: gateway/server、新 API、前端资源托管、app 装配。

### 4. 目录布局与命名怎么落？

**Recommended answer:** internal/core + internal/backend + 顶层 frontend/（推荐）

**User answer:** internal/core + internal/backend + 顶层 frontend/（推荐）

**Status:** resolved

**Notes:** 保持「除 cmd/ 外全部包在 internal/」规则；嵌入资源留在 backend 包内以符合 go:embed 限制。

### 5. 配置接口给到哪一档？

**Recommended answer:** 只读 + 密钥脱敏（推荐）

**User answer:** 只读 + 密钥脱敏（推荐）

**Status:** resolved

**Notes:** GET /api/config 返回分组字段值与「密钥是否已配置」，绝不回传明文；写配置与热生效本轮不做。

### 6. 交互接口本轮要暴露哪些？（多选）

**Recommended answer:** 先做前三项：播报注入、会话读写、运行状态

**User answer:** 播报注入 / 说话（/inject 同能力）；会话：列出渠道会话、查看历史、以观众身份发一条消息；运行状态：接入客户端连接、播报队列深度、上报管线统计；记忆：查询 / 新增 / 删除条目；模型与表情：切换 Live2D 模型、表情预览；TTS：列出引擎与音色、试听；推流：开播 / 关播 / 状态；工具：列出已注册工具、直接调用（全选 8 类）

**Status:** resolved

**Notes:** 8 类接口全选，含写操作与子系统重建类（推流生命周期、模型切换、记忆删除、工具调用）；是否分批待下一轮澄清。

### 7. 接口形态走哪种？

**Recommended answer:** REST(/api/*) + /client-ws 仅推送（推荐）

**User answer:** REST(/api/*) + /client-ws 仅推送（推荐）

**Status:** resolved

**Notes:** 请求-响应走 HTTP JSON，推送与回执仍走既有 WS；同源，无 CORS 问题。

### 8. 新 API 的暴露面与鉴权？

**Recommended answer:** 全部限回环（推荐）

**User answer:** 不鉴权，沿用当前局域网信任模型

**Status:** resolved

**Notes:** 与 /inject 现状一致、改动最少；但结合已选的写操作接口（开播/关播、记忆删除、工具调用、替观众发消息），局域网内任何设备都能调用，风险面需在下一轮确认是否收窄。

### 9. 现有对外契约是否必须保持兼容？

**Recommended answer:** 保持兼容，新接口并存（推荐）

**User answer:** 允许破坏式改名，同批更新文档与页面

**Status:** resolved

**Notes:** 路径统一到 /api/* 口径，外部调用方（/inject 调用者）与内置页面须同批更新；具体路径命名与旧别名策略待下一轮澄清。

### 10. 迁移节奏与验证门槛？

**Recommended answer:** 搬目录 → 加 API → 改前端，每步全绿（推荐）

**User answer:** 搬目录 → 加 API → 改前端，每步全绿（推荐）

**Status:** resolved

**Notes:** 每步一个聚焦提交，第一段要求零行为变化；与「本轮不动页面」的时序冲突待澄清。

### 11. core→backend 的单向依赖怎么强制？

**Recommended answer:** 约定 + import 边界测试（推荐）

**User answer:** 约定 + import 边界测试（推荐）

**Status:** resolved

**Notes:** AGENTS.md 明文 + 一条单测遍历包依赖：core 出现 net/http/websocket 或 core 反向 import backend 即失败。

### 12. 本轮 grill-me 结束后产出什么？

**Recommended answer:** 只落规格/决议文档（推荐）

**User answer:** 决议 + 直接执行重构

**Status:** resolved

**Notes:** 决议落盘后直接开工，按「搬目录 → 加 API → 改前端」分步提交。

### 13. 文档同批更新范围？

**Recommended answer:** 全部同批更新 + 新增 API 契约文档（推荐）

**User answer:** 全部同批更新 + 新增 API 契约文档（推荐）

**Status:** resolved

**Notes:** AGENTS.md 模块划分与依赖方向、README 目录与快速开始、既有契约文档的路径引用同批改，新接口另写 API 契约文档。

### 14. 本轮决议写到哪里？

**Recommended answer:** 旧文件归档到 docs/archive/，新决议写 GRILL-ME.md（推荐）

**User answer:** 旧文件归档到 docs/archive/，新决议写 GRILL-ME.md（推荐）

**Status:** resolved

**Notes:** 先 git mv GRILL-ME.md 到 docs/archive/（如 grill-me-2026-09-19-repo-restructure.md），新决议写入默认 GRILL-ME.md。

### 15. 冲突点：你选了「不鉴权、沿用局域网信任」，但又选了写操作（开播/关播、记忆删除、工具调用、替观众发消息）。写接口暴露面怎么定？

**Recommended answer:** 写操作限回环，只读放开（推荐）

**User answer:** 全部开放，一律不鉴权

**Status:** resolved

**Notes:** 用户在明确知晓风险后选择全开放：开播/关播、记忆删除、工具调用、替观众发消息均不鉴权，暴露面由 [server].addr 绑定地址决定。

### 16. 冲突点：前端形态选了「同仓库前端工程 + 产物 embed」，技术栈选了「本轮不动页面」，迁移节奏又写了「第三步改前端」。本轮前端落到哪一步？

**Recommended answer:** 建 frontend/ 并搬迁页面源码，UI 不变（推荐）

**User answer:** 建 frontend/ 并搬迁页面源码，UI 不变（推荐）

**Status:** resolved

**Notes:** 本轮建顶层 frontend/（index.html、app.js、libs/），页面 UI 与逻辑不变；仅同步路径常量与 embed 位置。

### 17. 顶层 frontend/ 的源码怎么进到 backend 的 embed 包？

**Recommended answer:** go:generate 拷贝，产物 gitignore（推荐）

**User answer:** go:generate 拷贝，产物 gitignore（推荐）

**Status:** resolved

**Notes:** go:generate 把 frontend/ 拷到 internal/backend/web/，产物 gitignore；构建说明与 AGENTS.md 需写明「忘了 go generate 则嵌的是旧页面」。

### 18. 8 类交互接口一次全做，还是分批？

**Recommended answer:** 第一批四项，其余第二批（推荐）

**User answer:** 第一批四项，其余第二批（推荐）

**Status:** resolved

**Notes:** 第一批：GET /api/config、/api/speak、会话列表/历史/发消息、GET /api/status；第二批：记忆写、模型切换/表情、TTS 试听、推流控制、工具调用。

### 19. 破坏式改名后具体路径怎么定？

**Recommended answer:** 全量 /api/* + 页面占根（推荐）

**User answer:** 全量 /api/* + 页面占根（推荐）

**Status:** resolved

**Notes:** /inject→POST /api/speak；/client-ws→/api/client-ws；/web/→/（页面占根）；/live2d-models/→/api/models/；新接口均 /api/*；/login/* 保持限回环；旧路径不留别名。连带：推流渲染地址 /web/?autostart=1 需改为 /?autostart=1，e2e 浏览器脚本同批改。

### 20. core/ 内部子包名怎么处理？

**Recommended answer:** 原样搬，仅加 core/ 前缀（推荐）

**User answer:** 原样搬，仅加 core/ 前缀（推荐）

**Status:** resolved

**Notes:** core/{shared,config,logger,agent,tts,stream,gateway}，backend/{server,api,web}；搬迁步骤零语义变更、diff 只有 import 前缀。

### 21. 「以观众身份发一条消息」的语义怎么定？

**Recommended answer:** 走上传管线，等同平台事件，落记忆（推荐）

**User answer:** 走上传管线，等同平台事件，落记忆（推荐）

**Status:** resolved

**Notes:** POST /api/sessions/{id}/messages 带 {user_id,user_name,text}，经 upload 管线（去重/敏感词/门控一致）入会话并落记忆，等同「用 HTTP 模拟一个观众事件」。

### 22. 每步的验证门槛怎么定？

**Recommended answer:** 全绿 + e2e 冒烟 + 新 API 的 e2e 断言（推荐）

**User answer:** 全绿 + e2e 冒烟 + 新 API 的 e2e 断言（推荐）

**Status:** resolved

**Notes:** 每步 gofmt+build+vet+test 全绿 + e2e 冒烟，并新增 e2e 步骤用 HTTP 调 /api/* 断言响应；未选 -race，标记为待补风险。

### 23. 最后一问：本轮实际执行的边界到哪？

**Recommended answer:** 搬到第二步 + 同批路径切换，第二批下一轮（推荐）

**User answer:** 搬到第二步 + 同批路径切换，第二批下一轮（推荐）

**Status:** resolved

**Notes:** 本轮执行：第一步搬目录（core/backend + 边界测试 + embed 链路 + 文档），第二步新路由与第一批四项接口（/api/config、/api/speak、/api/sessions*、/api/status，旧路径此阶段仍可达），第三步一次性路径切换（旧路径下线 + 页面路径常量 + 推流渲染地址同批）。第二批（记忆/模型/TTS/推流/工具）下一轮。

## Agreed Decisions

- 前端形态：同仓库前端工程 + 构建产物 embed 回二进制，保住「部署只有一个可执行文件」的不变量
- 页面技术栈：本轮不动页面 UI 与逻辑，只搬迁源码与同步路径常量；配置/会话面板留下一轮
- core 边界：core 不 import net/http 与 websocket；core = shared/config/logger/agent/tts/stream + gateway/{event,upload,filter}；backend = gateway/server、api、web 资源托管、app 装配
- 目录布局：internal/core/ + internal/backend/ + 顶层 frontend/（保持「除 cmd/ 外全部包在 internal/」规则）
- 配置接口：只读 GET /api/config，密钥类一律脱敏为「是否已配置」，写配置与热生效本轮不做
- 交互接口范围：8 类全选（播报注入、会话读写、运行状态、记忆 CRUD、模型与表情、TTS 试听、推流控制、工具调用），但分批交付
- 接口形态：HTTP JSON 负责请求-响应（/api/*），/api/client-ws 保持只做推送与回执，两者同源无 CORS
- 鉴权与暴露面：/api/* 一律不鉴权，沿用当前局域网信任模型（写操作也不限回环）——经明确确认
- 契约：允许破坏式改名，外部调用方与内置页面同批更新
- 迁移节奏：搬目录 → 加 API → 一次性路径切换，每步一个聚焦提交、每步全绿
- 边界强制：AGENTS.md 明文 + 一条 import 边界测试（core 出现 net/http/websocket 或反向 import backend 即失败）
- 本轮产出：决议 + 直接执行重构，不止于文档
- 文档：AGENTS.md（模块划分与依赖方向）、README（目录与快速开始）、既有契约文档路径引用全部同批更新，并新增 API 契约文档
- 决议落盘：旧 GRILL-ME.md git mv 到 docs/archive/grill-me-2026-09-19-repo-restructure.md，新决议写入 GRILL-ME.md
- 写接口暴露面：全部开放，不鉴权（开播/关播、记忆删除、工具调用、替观众发消息均可局域网调用）
- 前端落点：本轮建顶层 frontend/（index.html、app.js、libs/）并搬迁页面源码，UI 不变，仅同步路径常量与 embed 位置
- 嵌入同步：go:generate 把 frontend/ 拷到 internal/backend/web/assets/，产物 gitignore，单一事实源
- 接口分批：第一批 = GET /api/config、/api/speak、会话列表/历史/发消息、GET /api/status；第二批 = 记忆写、模型切换/表情、TTS 试听、推流控制、工具调用
- 路径命名（全量 /api/* + 页面占根，旧路径不留别名）：/inject→POST /api/speak、/client-ws→/api/client-ws、/web/→/、/live2d-models/→/api/models/、新接口 /api/{config,sessions,status,memory,models,tts,stream,tools}、/login/* 与 /favicon.ico 保持、[[clients]].path 不变
- core 子包命名：原样搬、仅加 core/ 前缀（core/{shared,config,logger,agent,tts,stream,gateway}），backend/{server,api,web}；重命名另开一轮
- 会话写语义：POST /api/sessions/{id}/messages 带 {user_id,user_name,text}，经 upload 管线（去重/敏感词/门控一致）入会话并落记忆，等同「用 HTTP 模拟一个观众事件」
- 验证门槛：每步 gofmt+build+vet+test 全绿 + e2e 冒烟 + 新增 e2e 步骤用 HTTP 调 /api/* 并断言响应
- 本轮执行范围：第一步（搬目录 + 边界测试 + embed 链路 + 文档）+ 第二步（新路由与第一批四项接口，旧路径此阶段仍可达）+ 第三步（一次性路径切换：旧路径下线、页面路径常量、推流渲染地址同批）；第二批接口下一轮
- 路径切换的实现约束：为满足「每步全绿」，第二步先以新路径提供服务且旧路径保持可达，第三步才下线旧路径并同批改页面路径常量与推流渲染地址 /web/?autostart=1 → /?autostart=1

## Open Risks

- /api/* 全部无鉴权且含写操作（开播/关播、记忆删除、工具调用、替观众发消息），实际暴露面等于 [server].addr；局域网任意设备可开播或删数据。协议默认（未经单独确认）：启动日志显式提示「绑定地址即暴露面」，API 契约文档标注无鉴权姿态——如需收紧，改回写操作限回环即可（loopbackOnly 已存在）
- 验证门槛未含 go test -race；新 API 与播报/会话/推流并发，第二批引入推流可启停后竞态风险上升，建议第二批落地时补 -race
- go:generate 忘跑会导致二进制内嵌旧页面；需在构建说明与 AGENTS.md 写明，并让 e2e 断言页面路径可访问（否则漂移无人发现）
- 页面占根后 /web/ 书签失效；推流渲染地址 /web/?autostart=1 漏改会让推流渲染 404，画面直接断——已列入第三步同批清单
- 第一步的「零行为变化」需靠「diff 只有 import 前缀 + 全绿 + e2e」证明；若顺手重命名或改逻辑，这个基准即失效
- 前端「同仓库工程」在原生 JS 无构建前提下只能靠 go:generate 拷贝落地；下一轮若引入 Vite/框架，embed 流程（产物是否提交、生成时机、构建依赖）需重新定义
- 8 类接口的 4 项第二批各自牵动子系统重建（推流生命周期对象、模型切换重建 catalog、TTS 试听是否绕过队列、记忆删除要改追加式存储语义），这些细节尚未决策，需下一轮澄清

## Next Decision Needed

本轮无未决项。下一轮开始前需要决定的是第二批接口的实现细节：推流从「随进程起停」改为可启停生命周期对象的边界、模型切换是否需要重建前端 catalog/表情词表、TTS 试听是否绕过播报队列的冷却与抢占、记忆删除在 JSON Lines 追加式存储上的语义（重写文件 vs 墓碑标记）。
