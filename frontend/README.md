# 前端资产说明

这里是前端源码（单一事实源）。两套页面：

| 目录 | 内容 | 构建 |
| --- | --- | --- |
| `./`（根） | 舞台页：`index.html` + `app.js` 原生 JS，Live2D 出镜 + 本地输入框 | 无构建 |
| `debug/` | 调试台：Vite + Vue 3 + TypeScript（导播中控台，10 个面板） | 需要 Node |

页面资源由 `go generate ./...` 同步进 `internal/backend/web/assets/` 再被 `//go:embed` 嵌进二进制：
改完页面必须重跑一次 go generate，否则二进制里嵌的还是旧页面（产物目录 gitignore，不提交）。
部署本身仍只有一个可执行文件。
| 文件 | 来源 | 许可 |
| --- | --- | --- |
| `libs/pixi.min.js` | pixi.js 6.5.10（npm `pixi.js@6.5.10`，`dist/browser/pixi.min.js`） | MIT |
| `libs/cubism4.min.js` | pixi-live2d-display 0.4.0（npm `pixi-live2d-display@0.4.0`，`dist/cubism4.min.js`，已内含 Cubism Web Framework） | MIT |
| `libs/live2dcubismcore.min.js` | 复制自本机 `Open-LLM-VTuber/frontend/libs/`（Live2D Cubism Core 5.0.0） | Live2D Proprietary Software License Agreement |
| `debug/public/fonts/*.woff2` | Google Fonts 的 latin 子集：Chakra Petch 400/500/600、JetBrains Mono（可变字体） | SIL Open Font License 1.1 |

`live2dcubismcore.min.js` 是 Live2D 官方的核心运行库，不是开源软件；使用它即表示接受
Live2D 的许可条款。它只有在渲染 Live2D 模型时才需要，`backend/web` 不依赖它。

## 调试台（frontend/debug）

导播中控台风格的本机调试面板，挂在 `/debug/`（只允许本机访问），10 个面板：
概览 / 会话 / 播报 / 模型 / 记忆 / 工具 / 配置 / 日志 / 推流 / 对话记录。
依赖只有 `vue` + `vue-router`，图表是手绘 SVG；设计令牌见 `src/styles/tokens.css`。

```bash
cd frontend/debug
npm ci --registry=https://registry.npmmirror.com   # 首次
npm run dev      # 开发：http://localhost:5174/debug/ ，/api 代理到 127.0.0.1:6199
npm run build    # 构建 dist/（go generate 会自动跑这一步并拷进 assets/debug/）
```

- Node 位置：WSL 里是 `~/tools/node/bin`（generate 脚本会自动加进 PATH）；
  Windows 侧 `C:\Program Files\nodejs`
- 路由是 hash 模式：`/debug/#/logs` 这类深链可直接打开；对话记录支持
  `#/chats?platform=local&channel=room_local` 直选渠道
- 面板对未装配的能力显示「未启用」态（后端 503 降级），不做 mock 数据
- 主题：状态条右侧可切换黑夜/白天；默认跟随系统，手动切换后记住选择（localStorage）。
  分享/截图可用 `?theme=light` / `?theme=dark` 临时指定（不持久化），如
  `/debug/?theme=light#/logs`

## 为什么不直接用旧前端

原 `Open-LLM-VTuber/frontend/` 是一份 1.9 MB 的预构建 Vue 产物（子模块 build 分支），
需要配套实现 21 种下行消息、历史记录、配置切换等一整套协议才能跑起来。自研页面只认
本仓库需要的 2 种下行消息与 2 种上行回执，见 `internal/backend/web/doc.go` 的协议说明。

## 口型同步

`pixi-live2d-display` 本身不带 lip sync（`speak()` 是别的分支才有的 API），
所以 `app.js` 用 Web Audio 的 `AnalyserNode` 算音量，再在内部模型更新之后把音量写进
模型自己的 LipSync 组参数（mao_pro 是 `ParamA`，取不到就退回 `ParamMouthOpenY`）。
写在内层更新之后是为了盖住动作给的值，代价是口型比声音晚一帧。

## 本地文本输入

页面底部有输入框：回车或点「发送」会以观众身份把消息投进会话
（`POST /api/sessions/<渠道>/messages`，与平台事件同一条上传管线），
回复经播报队列回来出声、做表情。

- 渠道与身份可用 URL 参数覆盖：`?channel=group_123456&name=小明`，默认 `room_local` / `本地观众`
- `room_` 前缀走 B 站平台（没有文本下行动作，只出声）；`group_` 前缀走 QQ，
  若机器人已连接且渠道号是纯数字群号，回复也会发回 QQ 群
- 推流模式（`?autostart=1`）下输入框隐藏，避免入镜

## 浏览器自动播放

浏览器要求页面先有过一次用户交互才允许出声，因此页面有个「点击开始」遮罩，
点击之后才连 `/api/client-ws`——这样开始播报之前的内容不会被静默丢掉。
