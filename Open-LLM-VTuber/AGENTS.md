# 仓库指南

## 项目结构与模块组织

本项目是将基于Open-LLM-VTuber二次开发，构建一个面向 Bilibili Live、OneBot 和 VUP 的实时交互系统，优先保证单次响应速度和直播中的即时反应，考虑集群、多 Agent、复杂 RAG 和高并发扩容，将利用现存的语音模块和vup(我们会用rust重构)，接入层(使用go传递ws数据)，在做到尽可能的实时的情况下重构这个项目

Open-LLM-VTuber 是一个基于 Python FastAPI 的实时语音交互与 Live2D 头像应用。核心后端代码位于 `src/open_llm_vtuber/`。主要模块包括：`agent/` 负责 LLM 与 Agent 后端，`asr/`、`tts/`、`vad/` 负责音频相关引擎，`conversations/` 负责对话编排，`config_manager/` 负责类型化 YAML 配置加载，`live/` 负责 Bilibili Live 等直播平台接入。运行入口是 `run_server.py`，辅助脚本位于 `scripts/`。

资源和面向用户的配置放在 `src/` 外部：`characters/` 存放角色 YAML，`config_templates/` 存放默认配置，`live2d-models/` 存放 Live2D 模型，`prompts/` 存放提示词片段，`backgrounds/`、`avatars/`、`assets/` 存放媒体资源。`frontend/` 是前端子模块或客户端资源包，除非任务明确涉及前端，否则不要改动。

## 构建、测试与开发命令

- `uv sync`：根据 `pyproject.toml` 和 `uv.lock` 安装依赖。
- `uv run run_server.py`：启动本地服务。
- `uv run run_server.py --verbose`：以详细日志模式启动，便于调试。
- `uv run upgrade.py`：运行项目升级辅助逻辑。
- `ruff check .`：检查 Python 代码问题。
- `ruff format .`：格式化 Python 代码。
- `pre-commit run --all-files`：提交前运行质量检查。

Bilibili Live 功能需要先配置 `conf.yaml`，再运行 `uv run python scripts/run_bilibili_live.py`。

## 代码风格与命名约定

使用兼容 Python 3.10 的语法。WebSocket、ASR、TTS 和对话流程应沿用现有异步写法。新增引擎时，需要同时更新实现模块、工厂注册、配置模型和默认 YAML 模板。函数、模块和配置键使用 `snake_case`，类名使用 `PascalCase`。格式化和导入整理以 Ruff 为准。

## 测试指南

当前仓库没有独立的测试目录。变更后至少运行 `ruff check .`，并配合有针对性的手动验证。涉及引擎或 WebSocket 的改动，应通过 `uv run run_server.py --verbose` 验证相关流程，并在 PR 中说明实际测试内容。若新增逻辑可以脱离模型下载或外部服务独立运行，应补充聚焦测试。

## 提交与 Pull Request 规范

近期提交历史使用 `feat:`、`docs:`、`Fix:`、`Release:` 等简短前缀。提交标题应简洁、使用祈使语气，例如 `feat: add onebot live adapter`。PR 需要说明面向用户的变化、配置或依赖影响、关联 issue；涉及 UI、Live2D、音频或直播平台行为时，应附截图、日志或可复现说明。

## 安全与配置提示

不要提交 `conf.yaml` 中的密钥、API key、Cookie、Bilibili `SESSDATA`、生成的聊天记录、缓存文件或下载的模型权重。可复用默认值放在 `config_templates/`，本地凭据保存在私有配置中。
