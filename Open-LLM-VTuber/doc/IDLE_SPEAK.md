# 空闲自动发言（主动发起话题）文档

## 1. 概述

客户端空闲一段时间后，后端会自动触发一次主动发言（`ai-speak-signal`）：发言内容结合最近对话上下文，并可选的通过网络搜索（MCP 的 `ddg-search` 热点）生成话题。

该功能**默认关闭**（`enabled: False`），需要手动在 `character_config.proactive_speak_config` 中开启。即使开启，无 MCP 或搜索失败也不影响基于上下文的发言。

## 2. 配置说明

### 2.1 `character_config.proactive_speak_config`

Pydantic 模型位于 `src/open_llm_vtuber/config_manager/proactive_speak.py`，所有字段都有默认值，向后兼容：已有用户的 `characters/*.yaml` 与 `conf.yaml` 即使不配置该块也能正常通过校验。

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `enabled` | bool | `False` | 是否启用空闲自动发言 |
| `idle_timeout_seconds` | int | `120` | 空闲多少秒触发一次主动发言 |
| `interval_seconds` | int | `300` | 两次主动发言的最小间隔，防止刷屏 |
| `use_web_search` | bool | `True` | 是否使用网络搜索热点来生成话题 |
| `search_queries` | List[str] | `["AI", "科技", "今日热点"]` | 热点搜索关键词列表，多次触发时轮换使用 |
| `search_max_results` | int | `5` | 每次热点搜索最多获取的结果条数 |
| `prompt_key` | str | `idle_speak_prompt` | `system_config.tool_prompts` 中指向主动发言提示词文件的键名 |

### 2.2 `system_config.tool_prompts.idle_speak_prompt`

新增的系统配置键，指向 `prompts/utils/idle_speak_prompt.txt`。默认模板里已带有注释示例：

```yaml
system_config:
  tool_prompts:
    # 客户端空闲时主动发起话题的提示词；context 与可选热点会自动作为额外段落附加到该提示词之后
    idle_speak_prompt: 'idle_speak_prompt'
```

### 2.3 配置示例

```yaml
character_config:
  proactive_speak_config:
    enabled: true          # 启用空闲自动发言
    idle_timeout_seconds: 120  # 空闲 120 秒触发
    interval_seconds: 300      # 两次主动发言最小间隔 300 秒
    use_web_search: true       # 使用网络搜索热点
    search_queries:            # 热点搜索关键词（轮换使用）
      - "AI"
      - "科技"
      - "今日热点"
    search_max_results: 5      # 每次最多 5 条结果
    prompt_key: 'idle_speak_prompt'  # 指向 tool_prompts 里的提示词文件名
```

## 3. 工作原理

实现位于 `src/open_llm_vtuber/websocket_handler.py`。整体流程如下：

```
客户端活跃刷新最后活跃时间
        │
        ▼
后台检查任务（5s 轮询）逐个检查客户端
        │  满足：enabled 为真
        │       空闲 ≥ idle_timeout_seconds
        │       距上次发言人 ≥ interval_seconds
        │       无进行中对话（current_conversation_tasks 未 done）
        │       非群聊（群成员 ≤ 1）
        ▼
触发主动发言：提示词 + <recent_context> + 可选 <hot_topics>
        │
        ▼
通过 ai-speak-signal 路径发起对话（metadata 带 proactive_speak=True）
```

### 3.1 空闲检测

- 活跃消息类型集合 `ACTIVE_MESSAGE_TYPES = {text-input, mic-audio-end, mic-audio-data, raw-audio-data, ai-speak-signal, interrupt-signal}`。收到这些消息时视为客户端活跃，刷新该客户端的最后活跃时间 `client_last_active`。
- 后台任务 `_idle_speak_check_loop` 在**首个连接建立时启动**，以 5 秒（`IDLE_CHECK_INTERVAL`）周期轮询，调用 `_check_idle_speak_clients` 逐个检查所有已连接客户端；**最后一个客户端断开时取消**，避免悬挂。
- 检查条件全部满足才触发：`enabled` 为真、空闲时长 ≥ `idle_timeout_seconds`、距上次发言 ≥ `interval_seconds`、无进行中对话（`current_conversation_tasks` 对应该客户端未 done）、非群聊（群成员 ≤ 1）。

### 3.2 组装提示词

`_trigger_idle_speak` 依次组装 `user_input`：

1. 提示词：`_load_idle_speak_prompt`，优先取配置的 `prompt_key` 指向的 `tool_prompts` 文件，缺失时回退 `proactive_speak_prompt`，再回退默认英文提示词；
2. 最近对话上下文：`_get_recent_context`，取最近 6 条历史（用 `get_history`），按 `用户：…` / `AI：…` 格式拼接，包在 `<recent_context>` 标签中（无历史则为空）；
3. 网络搜索热点：`_fetch_hot_topics`（仅当 `use_web_search` 为真时），结果包在 `<hot_topics>` 标签中（失败/不可用则为空字符串）。

最终 `user_input = 提示词 + <recent_context> + <hot_topics>`，作为 `{"type": "ai-speak-signal", "text": user_input}` 发送。

### 3.3 触发对话

复用 `conversations/conversation_handler.py` 的 `ai-speak-signal` 路径发起对话，metadata 带 `proactive_speak=True, skip_memory=True, skip_history=True`（不入记忆、不入历史）。该函数已支持调用方显式传 `text` 时直接用作内容，否则回退到 `proactive_speak_prompt`（向后兼容）。

### 3.4 生命周期

- `handle_disconnect` / `_cleanup_failed_connection` 会清理该客户端的 `client_last_active` / `client_last_idle_speak` / `client_idle_speak_index`。

## 4. 网络搜索热点

### 4.1 前置条件

- `context.mcp_client` 存在；
- `ddg-search` 在 `agent_settings.basic_memory_agent.mcp_enabled_servers` 中；
- 若 `use_mcpp=False` 或服务器不含 `ddg-search`，则自动跳过热点搜索。

### 4.2 ddg-search

`mcp_servers.json` 中 `ddg-search` 使用 `uvx duckduckgo-mcp-server`：

```json
{
  "mcp_servers": {
    "ddg-search": {
      "command": "uvx",
      "args": ["duckduckgo-mcp-server"]
    }
  }
}
```

`agent_settings.basic_memory_agent.mcp_enabled_servers` 需包含 `ddg-search`：

```yaml
agent_config:
  agent_settings:
    basic_memory_agent:
      mcp_enabled_servers: ["time", "ddg-search"]
```

### 4.3 执行与降级

`_fetch_hot_topics` 流程：

1. 用 `list_tools("ddg-search")` 确认存在 `web_search` 工具；
2. 用 `call_tool("ddg-search", "web_search", {query, max_results})` 执行搜索，15 秒超时；
3. 查询词 `_pick_search_query` 从 `search_queries` 中轮换选取（列表为空时回退 `"今日热点新闻"`）。

以下情况均**优雅降级为空字符串**（仅使用上下文发言），不影响主动发言：

- MCP 客户端不存在或 `ddg-search` 不在启用列表中；
- `ddg-search` 未提供 `web_search` 工具；
- 搜索超时（15 秒）或抛出异常。

## 5. 提示词定制

提示词文件位于 `prompts/utils/idle_speak_prompt.txt`，内容指导 LLM 在用户沉默一段时间后主动开启话题：结合最近上下文与可选热点，说一句简短的话开启对话，**不要询问空闲时间本身**。

可通过 `character_config.proactive_speak_config.prompt_key` 指向 `system_config.tool_prompts` 中你自定义的键名来替换提示词文件。若该键缺失，依次回退 `proactive_speak_prompt`、默认英文提示词。

## 6. 相关实现位置

| 职责 | 文件 |
| --- | --- |
| 空闲自动发言配置模型（`ProactiveSpeakConfig`） | `src/open_llm_vtuber/config_manager/proactive_speak.py` |
| 空闲检测、后台检查任务、提示词组装、网络搜索热点 | `src/open_llm_vtuber/websocket_handler.py` |
| `ai-speak-signal` 对话触发路径（metadata 带 proactive_speak） | `src/open_llm_vtuber/conversations/conversation_handler.py` |
| 主动发言提示词文件 | `prompts/utils/idle_speak_prompt.txt` |
| 配置默认模板（`tool_prompts.idle_speak_prompt` 与 `proactive_speak_config`） | `config_templates/conf.ZH.default.yaml`、`config_templates/conf.default.yaml` |
| ddg-search MCP 服务器定义 | `mcp_servers.json` |