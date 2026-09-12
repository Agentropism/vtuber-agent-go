# 移除 Web Tool 设计

## 目标

移除不再使用的 Web Tool 页面及其专用 TTS WebSocket 接口，减少无用的静态资源和公开服务入口，同时保留当前语音对话和备用语音转写所需的能力。

## 范围

- 删除 `web_tool/` 目录中的静态页面、脚本和说明文档。
- 删除 `/web-tool`、`/web_tool` 页面入口和 `/web-tool` 静态目录挂载。
- 删除仅供 Web Tool 使用的 `/tts-ws` 接口。
- 保留核心 ASR、TTS 引擎及会话流程，不修改模型配置和初始化逻辑。
- 保留 `/asr` HTTP 接口，继续支持上传 16 位 PCM WAV 文件进行备用转写。
- 保留 `/live2d-models/info` 接口。

## 结构调整

将原来的 `init_webtool_routes()` 拆分为两个职责单一的路由工厂：

- ASR 路由工厂：注册 `/asr`，继续使用 `ServiceContext` 中已初始化的 ASR 引擎。
- Live2D 路由工厂：注册 `/live2d-models/info`，保持当前响应结构和扫描逻辑不变。

服务器继续无条件注册这两个保留路由，但不再挂载 Web Tool 静态目录。清理删除功能遗留的导入、模块说明和类说明，其他静态目录与 WebSocket 路由保持不变。

## 行为与兼容性

- `/client-ws`、`/proxy-ws`、主前端、Live2D 静态资源和缓存资源不受影响。
- 语音对话仍可直接调用 ASR/TTS 引擎。
- 外部调用方仍可通过 `POST /asr` 使用备用转写能力。
- `/web-tool`、`/web_tool` 和 `/tts-ws` 将不再可用，这是本次预期的不兼容变化。

## 错误处理

`/asr` 保持现有校验与错误响应，不改变音频格式要求。Live2D 模型目录不存在时继续返回现有的 404 JSON 响应。

## 验证

- 运行 `ruff check src/open_llm_vtuber/server.py src/open_llm_vtuber/routes.py`。
- 运行 `git diff --check`。
- 通过 FastAPI 路由表确认 `/asr` 和 `/live2d-models/info` 存在，`/web-tool`、`/web_tool` 与 `/tts-ws` 不存在。
- 确认启动配置不再依赖 `web_tool/` 目录。

## 非目标

- 不移除 ASR/TTS 引擎、配置模型或会话内语音能力。
- 不修改前端、Live2D 模型或其他本地未提交文件。
- 不引入新的功能开关或兼容重定向。
