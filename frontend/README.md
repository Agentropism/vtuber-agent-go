# 前端资产说明

这里是前端源码（单一事实源）：`index.html` + `app.js` 原生 JS，无构建步骤。

页面资源由 `go generate ./...` 同步进 `internal/backend/web/assets/` 再被 `//go:embed` 嵌进二进制：
改完页面必须重跑一次 go generate，否则二进制里嵌的还是旧页面（产物目录 gitignore，不提交）。
部署本身仍只有一个可执行文件。
| 文件 | 来源 | 许可 |
| --- | --- | --- |
| `libs/pixi.min.js` | pixi.js 6.5.10（npm `pixi.js@6.5.10`，`dist/browser/pixi.min.js`） | MIT |
| `libs/cubism4.min.js` | pixi-live2d-display 0.4.0（npm `pixi-live2d-display@0.4.0`，`dist/cubism4.min.js`，已内含 Cubism Web Framework） | MIT |
| `libs/live2dcubismcore.min.js` | 复制自本机 `Open-LLM-VTuber/frontend/libs/`（Live2D Cubism Core 5.0.0） | Live2D Proprietary Software License Agreement |

`live2dcubismcore.min.js` 是 Live2D 官方的核心运行库，不是开源软件；使用它即表示接受
Live2D 的许可条款。它只有在渲染 Live2D 模型时才需要，`backend/web` 不依赖它。

## 为什么不直接用旧前端

原 `Open-LLM-VTuber/frontend/` 是一份 1.9 MB 的预构建 Vue 产物（子模块 build 分支），
需要配套实现 21 种下行消息、历史记录、配置切换等一整套协议才能跑起来。自研页面只认
本仓库需要的 2 种下行消息与 2 种上行回执，见 `internal/backend/web/doc.go` 的协议说明。

## 口型同步

`pixi-live2d-display` 本身不带 lip sync（`speak()` 是别的分支才有的 API），
所以 `app.js` 用 Web Audio 的 `AnalyserNode` 算音量，再在内部模型更新之后把音量写进
模型自己的 LipSync 组参数（mao_pro 是 `ParamA`，取不到就退回 `ParamMouthOpenY`）。
写在内层更新之后是为了盖住动作给的值，代价是口型比声音晚一帧。

## 浏览器自动播放

浏览器要求页面先有过一次用户交互才允许出声，因此页面有个「点击开始」遮罩，
点击之后才连 `/client-ws`——这样开始播报之前的内容不会被静默丢掉。
