## 提交消息事件

`POST /api/v1/events`

根据消息召回相关记忆，并返回注入记忆后的消息文本。

### 请求体

Content-Type：`application/json`

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `user_id` | string | 是 | — | 用户标识；会尝试解析平台 ID、canonical UID 或显示名 |
| `text` | string | 是 | — | 当前消息原文 |
| `sender_name` | string | 否 | `""` | 发送者昵称 |
| `session_id` | string | 否 | `""` | 会话 ID；当前路由尚未实际使用 |
| `system_prompt` | string | 否 | `""` | 调用方原始系统提示词 |

```json
{
  "user_id": "user123",
  "text": "我之前说过喜欢什么冰淇淋？",
  "sender_name": "Hako",
  "session_id": "chat-001",
  "system_prompt": "你是一个聊天助手。"
}
```

### 响应

```json
{
  "ok": true,
  "modified_text": "注入记忆后的消息文本",
  "injected_count": 3,
  "recalled_count": 3
}
```

`modified_text` 没有可注入内容时可能为 `null`。`injected_count` 和 `recalled_count` 当前均取召回原子数量。

### 示例

```bash
curl -X POST 'http://localhost:8765/api/v1/events' \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"user123","text":"我喜欢什么？","sender_name":"Hako"}'
```

> 当前限制：该路由不会使用 `session_id` 写入完整对话，也不能独立完成基于对话轮次的自动记忆整理；它主要用于召回和上下文注入。

## 查询记忆

`GET /api/v1/memories`

该接口根据 `q` 和 `uid` 是否同时存在，在搜索模式与列表模式之间切换。

### 查询参数

| 参数 | 类型 | 默认值 | 限制 | 说明 |
| --- | --- | --- | --- | --- |
| `q` | string | `""` | — | 搜索关键词 |
| `uid` | string | `""` | — | 用户 ID；与 `q` 同时提供才进入搜索模式 |
| `page` | integer | `1` | ≥ 1 | 列表模式页码 |
| `size` | integer | `20` | 1–100 | 列表模式每页数量 |
