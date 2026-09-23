# Grill Me Results

Generated: 2026-09-23T15:08:28.484Z

## Plan

为项目加入cmd指令，用于快速配置/测试等基础内容

## Shared Understanding

计划「为项目加入 cmd 指令，用于快速配置/测试等基础内容」经五轮访谈（26 条决议，含一次冲突澄清）收敛，随后按新决定删除了其中的 e2e 部分。

**最终交付形态**：保持单二进制，用 cobra 挂四个子命令 —— run / config / doctor / version。裸跑不再启动服务而是打印帮助，服务改为 run；新增 persistent 的 --config（默认 ./config.toml），配置里的相对路径仍以当前工作目录为基准。

config 只做 init（运行时读 ./config.toml.example 复制；已存在则拒绝，除非 --force；权限 0600）与 show（脱敏实现与 /api/config 共用一份）。配置合法性校验统一归 doctor：纯离线、只读（不建目录、不开文件、不外呼）、按配置推导必需项，含配置结构、角色资产、目录与既有数据、端口占用（bind 探测）、外部命令（仅 [stream].enabled 要 ffmpeg；renderer=true 且 input≠test 才要 Xvfb/Chrome）、凭据是否就位（不验证有效性）、前端模型目录与清单；退出码 0=全通过、1=有失败，警告不影响退出码。version 走包内默认常量 + 可选 ldflags 注入，未注入显示未知。

**e2e 部分已按后续决定整体移除**：曾把 scripts/e2e/ 迁进 internal/e2e（假服务与断言全部 Go 化、去掉 python3/curl），实现并经两轮真跑通过（8 通过 / 0 失败 / 1 跳过，整轮 4.7 秒）；但评估后认为它唯一的独有价值（装配启动验证、WS 回执门控、app.js 真跑、进程生命周期）不足以支撑约 2500 行的维护面，故连同 e2e 子命令一并删除，scripts/e2e/ 的四个文件已删、不再提供自动化冒烟。随之删除了 boundary_test 里针对 internal/e2e 的那条边界规则，并把 docs/BILIBILI_INGEST.md 的「冒烟里的替身」改写为手工验证说明。

配套仍在生效：docs/CLI.md 作为对外契约（子命令、参数、退出码、--json 字段名，新增字段允许、改名删除算破坏性变更）；README 的快速开始/验证/目录/文档表已同步（验证段现在只保留 go generate + gofmt + build + vet + test）。

## Questions and Answers

### 1. 当前 CLI 入口形态是什么（是否已有参数解析、裸跑行为）？

**Recommended answer:** 保持「裸跑=启动服务」不动，新增子命令时自行引入分发层

**User answer:** 代码事实：cmd/vtuber-agent-go/main.go 不解析任何参数（全仓库非测试代码里没有 os.Args / flag 用法），裸跑 = 装配并启动 HTTP 服务，靠 SIGINT/SIGTERM 优雅退出。

**Status:** resolved

**Notes:** 「裸跑=启动」是既有契约：README 快速开始与 scripts/e2e/run.sh 都按裸跑直接执行二进制写的。新增子命令必须自己引入分发层。

### 2. 配置从哪里读？是否已有可指定路径的能力？

**Recommended answer:** 沿用现状；需要指向别的配置时再加 --config

**User answer:** internal/core/config/config.go 的 ProvideConfig() 硬编码读工作目录的 config.toml（每次重读，便于运行时改配置）；6 个凭据可由环境变量兜底：LLM_API_KEY / OPENAI_API_KEY / SILICONFLOW_API_KEY / FISH_API_KEY / MINIMAX_API_KEY / BILIBILI_COOKIE。没有路径参数，也没有 SYAGENT_CONFIG 之类环境变量。

**Status:** resolved

**Notes:** ProvideConfig() 无参数版本被 app.Initialize() 直接调用；若要支持 --config，需要另加一个可传路径的入口而保持旧函数不动。

### 3. 现在「配置 / 测试」这件事是怎么做的？

**Recommended answer:** 承认现状是一组手工步骤 + bash 脚本，命令化的价值在于把它们收敛成稳定入口

**User answer:** 散落在 README 手工步骤与 bash 脚本里：go generate ./...（前端资源同步，由 internal/backend/web/handler.go 的 //go:generate 调 scripts/generate-web-assets.sh）、gofmt -l . && go build ./... && go vet ./... && go test ./...、scripts/e2e/run.sh（真二进制 + 假 LLM/假 TTS + 真 WS 客户端，需要 python3/curl，约数十秒）。

**Status:** resolved

**Notes:** 另有零散的内置自检开关：[stream].input = "test"（ffmpeg 测试画面，不需要 Xvfb/Chrome）、[[clients]] 决定接受哪些平台。

### 4. 仓库结构约定对 cmd/ 有什么限制？

**Recommended answer:** 遵守「cmd/ 只放单二进制入口」的既有约定

**User answer:** README 与 AGENTS.md 定：除 cmd/ 外全部包在 internal/，产物是单个二进制；cmd/vtuber-agent-go 是唯一不在 internal/ 下的包。依赖方向由 internal/backend/app/boundary_test.go 强制为 backend → core，反向只能用函数注入破环。

**Status:** resolved

**Notes:** 新增多个 cmd/* 可执行包会推翻该约定，README「目录」段与构建/分发方式需同批更新。子命令逻辑若放进 cmd/ 内则不受 boundary_test 约束（它只测 internal 内部）。

### 5. 有哪些现成实现可以直接复用，哪些反而会踩坑？

**Recommended answer:** 复用 tts.Chain 做试听；不要拿 app.Initialize() 当自检，因为装配有真实副作用

**User answer:** 可直接复用：internal/core/tts 的 Chain.Synthesize(ctx, text) 直接产音频（internal/backend/api/tts.go 的 /api/tts/preview 是其 HTTP 等价物），所以命令行试听成本很低。会踩坑：app.Initialize() 没有 dry-run 路径，装配即真实落副作用——memory.Open 会 MkdirAll 建目录（internal/core/agent/memory/memory.go:80），archive 同理（archive.go:104/159），「只校验不落地」必须独立实现。

**Status:** resolved

**Notes:** Initialize() 失败即 fast-fail（配置、角色文件、模型目录缺失都会在启动阶段报错），这正是「快速自检」想要覆盖的场景，但需要绕开它的副作用。

### 6. 「cmd 指令」的交付形态选哪种？（推荐「单二进制内嵌子命令」：README 已定「cmd/ 是唯一不在 internal/ 下的包、产物是单个二进制」，在同一个二进制内加子命令不推翻该约定，也不引入新的分发方式）

**Recommended answer:** 单二进制内嵌子命令

**User answer:** 单二进制内嵌子命令

**Status:** resolved

**Notes:** 扩展 cmd/vtuber-agent-go/main.go，保持 cmd/ 只有这一个非 internal 包与单产物分发。

### 7. 本轮具体要哪些子命令？（推荐 config + doctor + version 三件套：覆盖「快速配置」与「不用开场就能知道为什么起不来」，成本最低；tts/llm/e2e 属于会真实联网或耗时的部分，可后续追加）

**Recommended answer:** config + doctor + version 三件套

**User answer:** config（生成/校验配置）、doctor / check（环境与配置自检）、version（版本信息）

**Status:** resolved

**Notes:** 本轮不含 tts/llm/dev/e2e/memory，命令集保持最小。

### 8. 子命令用什么解析？（推荐「标准库 flag + 手写 dispatch」：项目目前 0 个 CLI 依赖、只有 6 个直接依赖，风格很克制）

**Recommended answer:** 标准库 flag + 手写 dispatch

**User answer:** 引入 cobra

**Status:** resolved

**Notes:** 推翻推荐：接受新增 cobra 直接依赖（会带来 spf13/pflag、spf13/cobra、mousetrap 等传递依赖），go.mod/go.sum 需同批更新。cobra 自带 completion 子命令与英文帮助，需要再定默认命令与帮助语言。

### 9. 不带参数的裸跑行为怎么定？（推荐「裸跑 = 启动服务（现状不变）」：README 快速开始、scripts/e2e/run.sh 都按它写的）

**Recommended answer:** 裸跑 = 启动服务（现状不变）

**User answer:** 裸跑打印帮助，服务改为 run 子命令

**Status:** resolved

**Notes:** 破坏既有契约：README 快速开始的 `./vtuber-agent-go` 一行、scripts/e2e/run.sh 里裸跑二进制的写法都要同批改成 `run`，否则冒烟会失败（脚本还会断言退出日志与端口释放）。

### 10. 配置文件路径要不要可指定？（推荐「加 --config，默认仍是 ./config.toml」）

**Recommended answer:** 加 --config，默认 ./config.toml

**User answer:** 加 --config，默认 ./config.toml

**Status:** resolved

**Notes:** 需要在 config 包新增可传路径的入口，旧 ProvideConfig() 保持不动以免影响 app.Initialize()。仍有一个未定问题：--config 指向别的目录时，配置里的相对路径（characters/、data/）以 cwd 还是配置文件所在目录为基准。

### 11. 「测试」类命令覆盖到哪一层？（推荐「离线校验 + 端到端冒烟」）

**Recommended answer:** 离线校验 + 端到端冒烟

**User answer:** 离线校验（不联网、不花钱）

**Status:** resolved

**Notes:** 本轮不做联网自检与 e2e 子命令：doctor 只做离线校验，凭据只判「是否就位」不验证有效性；端到端冒烟继续由 scripts/e2e/run.sh 承担。

### 12. 新命令与现有 scripts/*.sh 的关系怎么定？（推荐「命令内部调用现有脚本」，避免两套实现漂移）

**Recommended answer:** 命令内部调用现有脚本（脚本仍是唯一实现）

**User answer:** 迁移进 Go，废弃对应脚本

**Status:** resolved

**Notes:** 与所选命令集存在口径冲突，需澄清：本轮的 config / doctor / version 都不对应现有脚本——scripts/generate-web-assets.sh 仍由 go:generate 驱动，scripts/e2e/run.sh 承担端到端冒烟。要么该决议是面向后续轮次的方针，要么本轮就要把某个脚本迁进来。

### 13. 输出格式与退出码怎么定？（推荐「默认人读文本 + --json 可选，退出码 0 = 通过 / 非 0 = 失败」）

**Recommended answer:** 默认人读文本 + --json 可选

**User answer:** 默认人读文本 + --json 可选

**Status:** resolved

**Notes:** 每个命令要多写一份结构化输出与测试；--json 下不能混入人读提示语，否则 CI 无法解析。

### 14. 按 AGENTS.md，配套文档与测试产出到什么程度？（推荐「docs/CLI.md 契约文档 + 先写测试」）

**Recommended answer:** docs/CLI.md 契约文档 + 先写测试（TDD）

**User answer:** docs/CLI.md 契约文档 + 先写测试（TDD）

**Status:** resolved

**Notes:** 走 docs/ 顶层契约文档路线（与 docs/API.md 同级），不铺 effort 目录；README 的文档表需加一行。先写测试再写命令。

### 15. 运行平台边界怎么定？（推荐「只保证 Linux / WSL」）

**Recommended answer:** 只保证 Linux / WSL

**User answer:** 只保证 Linux / WSL

**Status:** resolved

**Notes:** 与主程序及 scripts/ 现状一致；命令依赖 bash/curl/python3 或 /proc、syscall 时不做 Windows 兼容分支。

### 16. go generate 与「跑哪个二进制」怎么统一？（推荐「默认 exec 自身；--rebuild 时先 go generate + go build 再跑新品」：默认跑真实产物且不依赖 node，需要新鲜度时用一个开关同时做 generate + build）

**Recommended answer:** 默认 exec 自身；--rebuild 时先 go generate + go build 再跑新品

**User answer:** 默认 exec 自身；--rebuild 时先 go generate + go build 再跑新品

**Status:** resolved

**Notes:** 解掉第三轮的冲突：go generate 重建的是前端资源，而资源已 embed 进正在运行的二进制，故「总是先 generate + exec 自身」无效。默认路径 = exec os.Executable()（快、不依赖 node/bash）；--rebuild 时先 go generate ./... + go build 到临时目录，再跑新产物（该路径需要 go 工具链 + bash + node，与 assets-keep 决议共存）。

### 17. doctor 怎么判定端口被占？（推荐 bind 探测：那条「无副作用」决议的实质是不建目录、不开文件、不联网，而端口可用性只有真 bind 一次才答得准）

**Recommended answer:** bind 探测（瞬时占用后立即释放）

**User answer:** bind 探测（瞬时占用后立即释放）

**Status:** resolved

**Notes:** 对第二轮「独立只读实现（无副作用）」的边界澄清：无副作用的含义是「不建目录、不开文件、不外呼」，瞬时 bind 再立即释放属于允许范围。理由：TIME_WAIT、SO_REUSEADDR、只监听 v6 这些情况扫 /proc 会看错，而端口能不能用正是启动时真正会碰到的错误。

### 18. e2e 的调参入口怎么定？（推荐「参数为主，旧环境变量作为回退」：参数化后才在 --help 里看得到，同时不弄坏已有用法与 CI）

**Recommended answer:** 参数为主，旧环境变量作为回退

**User answer:** 参数为主，旧环境变量作为回退

**Status:** resolved

**Notes:** 参数为主（如 --models-dir / --models-dict / --gw-port 等），SMOKE_MODELS_DIR / SMOKE_MODEL_DICT 等旧变量保留为回退，避免 README 与 docs/BILIBILI_INGEST.md 里教的用法失效。

### 19. doctor 的外部命令怎么判「必需」？（推荐按配置推导必需项，并用配置里声明的可执行名去 LookPath）

**Recommended answer:** 按配置推导必需项，只报真必需的

**User answer:** 按配置推导必需项，只报真必需的

**Status:** resolved

**Notes:** 从配置推导必需项：仅 [stream].enabled 时要求 ffmpeg；仅 renderer=true 且 input≠test 时额外要求 Xvfb 与 Chrome；可执行名取配置字段（ffmpeg/xvfb/chrome），留空回退默认值 "ffmpeg" / "Xvfb" / "google-chrome-stable"。python3 随 e2e Go 化从依赖中消失，node 只在 go generate 时需要，两者都不计入失败。

### 20. config show 的脱敏怎么做？（推荐提到共享实现：脱敏已存在于 backend/api 但是 unexported，两份规则比一份更容易漂）

**Recommended answer:** 提到共享实现，config show 与 /api/config 共用

**User answer:** 提到共享实现，config show 与 /api/config 共用

**Status:** resolved

**Notes:** 把脱敏逻辑（secretKeys = {api_key, cookie} + {\"configured\": bool} 形状）从 internal/backend/api 的 unexported 实现提到共享位置（core/config 或 api 导出函数），config show 与 /api/config 共用一份。/api/config 的响应结构不变，docs/API.md 契约不破，e2e 既有的脱敏断言继续有效。

### 21. version 输出哪些字段？（推荐「版本 + commit + 构建时间 + go 版本」：正好是排障真正需要的；OS/架构属于运行时环境，不作为版本契约）

**Recommended answer:** 版本 + commit + 构建时间 + go 版本

**User answer:** 版本 + commit + 构建时间 + go 版本

**Status:** resolved

**Notes:** 人读输出与 --json 都给这四个字段；未注入 ldflags 时 commit 与构建时间明确显示为未知，不造假值。版本默认常量 + 可选 -ldflags -X 注入（不新增构建脚本）。

### 22. 新增 internal/e2e 后要不要加可执行边界检查？（推荐加一条：core 与 backend 不得 import internal/e2e）

**Recommended answer:** 给 boundary_test.go 加一条：core 与 backend 不得 import internal/e2e

**User answer:** 给 boundary_test.go 加一条：core 与 backend 不得 import internal/e2e

**Status:** resolved

**Notes:** 沿用现有机制（boundary_test.go 已是 core 边界的可执行检查），给 internal/backend/app/boundary_test.go 增加一条：internal/core 与 internal/backend 下的包不得 import internal/e2e，防止生产代码反向依赖测试脚手架。

### 23. --json 输出的稳定性要不要当契约写？（推荐写进 docs/CLI.md：字段一旦被脚本消费就是对外接口）

**Recommended answer:** 字段名写进契约，新增字段允许、改名删除算破坏性变更

**User answer:** 字段名写进契约，新增字段允许、改名删除算破坏性变更

**Status:** resolved

**Notes:** docs/CLI.md 把 doctor 检查项名、config show 字段、e2e 结果字段写为契约；允许新增字段，改名与删除算破坏性变更。与之配套：e2e 的落盘断言从 grep 日志改为断言结构化结果。

### 24. e2e 的前端/模型目录默认怎么定？（推荐保持现状：默认探测仓库外的 ../Open-LLM-VTuber/live2d-models，存在则启用）

**Recommended answer:** 默认探测仓库外的 ../Open-LLM-VTuber/live2d-models，存在则启用

**User answer:** 默认探测仓库外的 ../Open-LLM-VTuber/live2d-models，存在则启用

**Status:** resolved

**Notes:** 保持现有行为：默认探测 $REPO_DIR/../Open-LLM-VTuber/live2d-models（与 SMOKE_MODELS_DIR 回退并存），存在则启用前端断言，否则只验证到播报队列。迁移不改变覆盖率。

### 25. 冲突澄清：最终命令集与 sc-migrate 的作用范围怎么定？（第一轮定的是 config/doctor/version，且 sc-migrate 当时没有可迁移的对象；第二轮把 e2e 也纳入了本轮）

**Recommended answer:** 命令集定为 run / config / doctor / version / e2e；sc-migrate 只管 scripts/e2e/

**User answer:** 最终命令集 = run / config（init + show）/ doctor / version / e2e；sc-migrate 收窄为只迁移并废弃 scripts/e2e/（run.sh、fakes.py、client、browser），scripts/generate-web-assets.sh 保留

**Status:** resolved

**Notes:** 冲突澄清（第二轮 e2e-bridge = 顺带把 e2e 迁移成 Go 子命令，覆盖第一轮 command-set 与 sc-migrate 的原口径）：最终命令集 = run / config / doctor / version / e2e（在 config+doctor+version 之外追加 run 与 e2e）；sc-migrate 的落点收窄为「只针对 scripts/e2e/」——run.sh、fakes.py、client、browser 全部迁入 internal/e2e 后删除，scripts/generate-web-assets.sh 明确保留（第三轮 assets-keep）。连带影响：README「快速开始/验证/目录」段、docs/BILIBILI_INGEST.md 对 scripts/e2e 的引用、go.mod（cobra）都需同批更新。

### 26. config init 的模板从哪里来？（推荐「运行时读 ./config.toml.example」：零副本、模板永远与仓库同步，且与程序既定的「相对 cwd 读配置」方式一致）

**Recommended answer:** 运行时读 ./config.toml.example

**User answer:** 运行时读 ./config.toml.example

**Status:** resolved

**Notes:** 技术限制：go:embed 不能引用包外路径（不能嵌 ../../../config.toml.example）。因此 config init 运行时读工作目录的 ./config.toml.example，文件不存在则以可读错误退出并提示来源；不采用 go:generate 复制 + embed（会让 go build 变成必须先跑 go generate），也不采用 Go 常量副本（两份事实源）。与「config.toml、characters/、data/ 全相对 cwd」的既定运行方式一致。

## Agreed Decisions

- 交付形态：单二进制内嵌子命令（扩展 cmd/vtuber-agent-go/main.go），保持 cmd/ 为唯一非 internal 包与单产物分发；不新增多个 cmd/* 可执行包
- 解析层：引入 cobra，接受 spf13/pflag、mousetrap 等传递依赖，go.mod/go.sum 同批更新
- 命令集：run / config / doctor / version / e2e（config、doctor、version 为第一轮原定；run 由「裸跑改帮助」带出；e2e 由第二轮追加）
- 裸跑行为：裸跑打印帮助，服务改为 run 子命令；README 快速开始与所有启动方式同批改为 run
- 帮助文本中文；隐藏 cobra 自带的 completion 子命令
- 配置路径：新增 persistent 的 --config，默认 ./config.toml；不做 SYAGENT_CONFIG 环境变量
- 相对路径基准：沿主程序语义，characters/、data/ 等仍以当前工作目录为基准（不改变 app 装配行为）
- 平台边界：只保证 Linux / WSL，不做 Windows 兼容分支
- config 职责：config = init + show；配置合法性校验统一归 doctor，不重复实现
- config init：复制 ./config.toml.example；目标已存在则拒绝，需显式 --force 才覆盖
- config init 模板来源：运行时读工作目录的 ./config.toml.example（go:embed 不能引用包外路径，故不做 generate+embed，也不做 Go 常量副本）
- doctor 实现口径：独立只读实现，不建目录、不开文件、不外呼；不复用 app.Initialize()（装配会真实 MkdirAll 建目录、打开 JSONL）
- 「无副作用」的边界澄清：瞬时 bind 端口探测属于允许范围
- doctor 端口检测：用 bind 探测（net.Listen 后立即关闭），不扫 /proc/net/tcp（TIME_WAIT、REUSEADDR、仅 v6 会看错）
- doctor 检查项：配置结构与取值、角色资产、目录与既有数据文件（含 memory.jsonl 合法性）、监听端口占用、外部命令（按需）、凭据是否就位、前端模型目录与清单
- doctor 外部命令判据：仅 [stream].enabled 要求 ffmpeg；仅 renderer=true 且 input≠test 额外要求 Xvfb 与 Chrome；可执行名取 ffmpeg/xvfb/chrome 字段，留空回退 ffmpeg / Xvfb / google-chrome-stable；node 仅构建期需要、python3 随 e2e Go 化消失，都不计入失败
- doctor 退出码：0 = 全通过，1 = 有失败；警告不影响退出码（可用作 CI 门禁）
- 本轮不做「凭据与运行数据是否会被误提交」检查（chk-gitignore 未选）
- 脱敏复用：把 secretKeys = {api_key, cookie} 的 {"configured": bool} 脱敏逻辑从 internal/backend/api 的 unexported 实现提到共享位置，config show 与 /api/config 共用一份
- /api/config 的响应结构与 docs/API.md 契约不变，e2e 既有的脱敏断言继续有效
- version 版本来源：包内默认常量 + 可选 -ldflags -X 注入，不新增构建脚本
- version 输出字段：版本 + commit + 构建时间 + go 版本；未注入时明确显示未知，不造假值
- e2e 落点：新增 internal/e2e（含子包），cmd 只做薄入口
- sc-migrate 收窄为只针对 scripts/e2e/：run.sh、fakes.py、client、browser 全部迁入 internal/e2e 后删除
- 假服务与断言全部 Go 化：假 LLM/假 TTS 用进程内 net/http，接口断言用 net/http client，去掉 python3 与 curl 依赖
- e2e 被测进程：exec os.Executable() 在临时目录里以 run 启动（保留优雅退出与端口释放断言，不依赖 go 工具链）
- e2e 资源新鲜度：默认不跑 go generate；--rebuild 时先 go generate ./... + go build 到临时目录再跑新产物
- e2e 断言范围（等价保留六类）：四条对话链路、音频契约与 playback-finished 回执、/api/* 接口断言、落盘与日志断言、浏览器检查（缺 playwright 则跳过）、优雅退出与端口释放
- e2e 落盘与日志断言改为断言结构化结果，而不是 grep 自由文本日志
- e2e 调参：cobra 参数为主，SMOKE_MODELS_DIR / SMOKE_MODEL_DICT 等旧环境变量作回退
- e2e 模型目录默认：探测 $REPO_ROOT/../Open-LLM-VTuber/live2d-models，存在则启用前端断言，否则只验证到播报队列（覆盖率与现状一致）
- e2e 失败处理：成功清理临时目录；失败保留并打印路径与关键日志尾部
- 输出契约：默认人读文本 + --json 可选；退出码 0 = 通过、非 0 = 失败
- JSON 契约：--json 字段名写进 docs/CLI.md（doctor 检查项名、config show 字段、e2e 结果字段），允许新增字段，改名与删除算破坏性变更
- 文档与测试：新增 docs/CLI.md 契约文档 + 先写测试再实现（TDD）；不铺 effort 目录；README 的文档表与快速开始/验证/目录段同批更新
- boundary_test.go 增加一条可执行边界：internal/core 与 internal/backend 下的包不得 import internal/e2e
- scripts/generate-web-assets.sh 本轮不迁移，go:generate 继续用它
- 【后续决定，取代上面 e2e 相关各条】e2e 子命令与 internal/e2e 整体删除：评估后认为它唯一的独有价值（装配启动验证、WS 回执门控、app.js 真跑、进程生命周期）不足以支撑其维护面；scripts/e2e 已删除，不再提供自动化冒烟；随之删除 boundary_test 里那条针对 internal/e2e 的边界规则
- 【后续决定】保留 run / config / doctor / version 四个子命令与 docs/CLI.md 其余契约；docs/BILIBILI_INGEST.md 的「冒烟里的替身」改为「验证上报」的手工验证说明

## Open Risks

- 【已接受的代价】删除 e2e 后，以下四类覆盖归零：①「二进制能否起来」（internal/backend/app 没有 Initialize() 的测试）；②WS 回执门控（internal/backend/server 仅 3 条测试，broadcast 的 14 条单测不含 playback-finished 往返）；③app.js 真跑（web_test.go 只测 HTTP 层，不执行 JS）；④进程生命周期（SIGTERM 优雅退出与端口释放）
- 裸跑行为变更会打破既有引用：README 快速开始、docs/BILIBILI_INGEST.md 都按裸跑直接执行二进制写的；已同批改为 run
- cobra 与仓库「6 个直接依赖、风格克制」的取向相悖，已引入 pflag/mousetrap 等传递依赖
- config init 依赖工作目录存在 ./config.toml.example；二进制离开仓库且旁边没放模板时会以错误退出（已接受）
- 环境问题（已解决并留证）：工具链的 gopls/pi-lens 诊断缓存曾长期报「cobra is not in your go.mod」，而 go list -m / go build / go vet 全绿；清 ~/.pi-lens/projects/.../cache/ 后主动探测 clean=7 / findings=0
- 访谈记录文件 docs/grill-me-2026-09-23-cli-commands.md 曾在会话中途消失（未跟踪文件，git 无法恢复），已据 .pi/grill-me/state.json 的 26 轮状态重建；删除原因未查明（该扩展源码中无删除逻辑）

## Next Decision Needed

无待答问题。遗留待办只有落地顺序与提交方式：docs/CLI.md 契约先行已执行完毕；本轮全部改动尚未提交，需决定是否与「删除 e2e」同批提交。
