# 前端接口契约

后端对前端暴露的 HTTP 接口。**请求-响应走 `/api/*`（JSON）**，推送（播报下发、播放回执）仍走
WebSocket `/api/client-ws`；两侧同源，没有 CORS。Live2D 页面占根路径 `/`。

| 项 | 约定 |
| --- | --- |
| 编码 | 请求与响应都是 JSON（`Content-Type: application/json`），字段名 snake_case |
| 请求体上限 | 64 KiB，超出由 `http.MaxBytesReader` 截断并回 `400 invalid json body` |
| 错误响应 | `{"error": "<描述>"}` |
| 未装配的能力 | 回 `503`（如未配 `[tts].engines` 时的播报、未配 `[llm]` 时的会话），不假装成功 |
| 鉴权 | **无**。能连上 `[server].addr` 就能调——包括写操作（播报、发消息、第二阶段的推流控制） |
| 暴露面 | 等于 `[server].addr` 的绑定范围：绑 `127.0.0.1` 只有本机，绑 `0.0.0.0` 局域网内任何设备可调 |

## 1. 配置只读

```http
GET /api/config
```

返回当前生效的配置快照，**结构与 `config.toml` 同构**（键名取自 toml tag，时间段渲染成 `"5m0s"`
这类字符串）。路由是先读盘再渲染，所以运行时改过配置文件后这里立刻能看到。

凭据类字段（`api_key`、`cookie`）不回明文，只回是否已配置：

```json
{
  "llm": {
    "base_url": "https://api.example.com/v1",
    "api_key": {"configured": true},
    "model": "deepseek-chat"
  },
  "stream": {"cookie": {"configured": false}, "cookie_file": "data/bilibili-cookie.txt"}
}
```

`cookie_file` 是路径不是凭据，按普通字符串返回。

| 状态码 | 含义 |
| --- | --- |
| 200 | 快照 |
| 500 | 读配置失败（文件缺失或 TOML 解析错误） |
| 503 | 装配时没有注入配置来源 |

## 2. 播报注入

```http
POST /api/speak
{"text": "要说的话", "emotion": "joy"}
```

把一段文本交给统一播报队列：合成语音、按优先级排队、推给前端出声。**入队即返回**——
`200` 只表示进了队列，不代表已合成、更不代表已播完。

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `text` | 是 | 要播报的文本；裁剪首尾空白后不能为空 |
| `emotion` | 否 | Live2D 表情标签（模型 `emotionMap` 的英文名），空表示不换表情；取值是否可用由模型决定，网关不校验 |

注入条目以 `proactive` 优先级入队：低于弹幕、礼物与醒目留言，高于待机发言。

| 状态码 | 响应体 | 含义 |
| --- | --- | --- |
| 200 | `{"status":"ok"}` | 已进入播报队列 |
| 400 | `{"error":"empty text"}` | `text` 为空或只有空白 |
| 400 | `{"error":"invalid json body"}` | 请求体不是合法 JSON |
| 405 | `{"error":"method not allowed"}` | 使用了 POST 以外的方法 |
| 503 | `{"error":"broadcast disabled"}` | 未配置 `[tts].engines`，播报链路没有装配 |
| 503 | `{"error":"..."}` | 队列已满，该条目因优先级最低被丢弃 |

## 3. 会话读写

### 3.1 列出会话

```http
GET /api/sessions
```

```json
{
  "count": 1,
  "sessions": [
    {
      "channel_id": "room_123456",
      "pending": 0,
      "history_len": 6,
      "last_active": "2026-09-21T10:00:00Z",
      "last_idle": "2026-09-21T10:01:00Z"
    }
  ]
}
```

`pending` 是该渠道待处理事件数（会话队列长度），`last_active` 是最近一次真实事件的时间，
`last_idle` 是最近一次主动发言的时间。未配置 `[llm]` 时返回 `503`。

### 3.2 查看历史

```http
GET /api/sessions/{channel_id}/history
```

```json
{"channel_id": "room_123456", "messages": [{"role": "user", "content": "你好"}]}
```

只回对话本身（`user` / `assistant`），**不回系统提示词**——那里面有人设细节。
渠道还没建立会话时回 `404`。

### 3.3 以观众身份发一条消息

```http
POST /api/sessions/{channel_id}/messages
{"user_id": "10086", "user_name": "接口调试", "text": "你好呀"}
```

它**走与平台事件同一条上传管线**（去重、敏感词、背压、分发门控一个不落），因此行为与真实弹幕
一致：会进会话、会生成回复、会写长期记忆、回复也会进播报队列。这是「用 HTTP 模拟一个观众事件」
的调试入口，不是第二条会话通道。

| 状态码 | 响应体 | 含义 |
| --- | --- | --- |
| 202 | `{"status":"accepted"}` | 已进管线（不等待回复生成完成） |
| 400 | `{"error":"empty text"}` | 文本为空 |
| 400 | `{"error":"unknown channel: ..."}` | 渠道号前缀不是 `group_` / `room_`，或对应平台没配在 `[[clients]]` |
| 503 | `{"error":"agent sessions disabled"}` | 未配置 `[llm]` |
| 503 | `{"error":"event pipeline has no handler"}` | 上传管线没有终端处理器 |

渠道号约定：`group_<群号>` 对应 `adapter_key = "onebot_v11"` 的客户端，`room_<房间号>` 对应
`adapter_key = "bilibili_live"` 的客户端；平台名取自 `[[clients]].platform`，所以自定义平台名也能工作。

## 4. 运行状态

```http
GET /api/status
```

```json
{
  "uptime": "1h2m3s",
  "started_at": "2026-09-21T09:00:00Z",
  "clients": {"count": 2, "platforms": ["bilibili", "qq"]},
  "broadcast": {"enqueued": 12, "played": 11, "preempted": 0, "dropped": 1, "failed": 0},
  "upload": {"queue_len": 0, "queue_size": 256, "dropped": 0},
  "sessions": 3
}
```

未装配的子系统对应字段直接不出现（例如没配 TTS 就没有 `broadcast`），而不是回 0 让人误以为可用。

## 5. 记忆读写

长期记忆是追加式 JSON Lines 文件（格式见 [`docs/MEMORY_API.md`](MEMORY_API.md)）；未配置
`[agent].memory_file` 时这一组接口返回 `503`。`id` 是记录在文件中的**行号**，追加式存储里稳定。

### 5.1 查询

```http
GET /api/memory?query=草莓&limit=5
```

带 `query` 时按关键词召回（打分规则见 MEMORY_API.md），不带时回最近若干条快照；
`limit` 缺省为召回 5 条 / 快照 20 条。

```json
{
  "count": 1,
  "total": 12,
  "entries": [
    {"id": 3, "time": "2026-09-21T11:13:49Z", "channel_id": "room_123456", "user": "观众甲", "text": "我说过喜欢草莓", "reply": "记住啦", "weight": 1}
  ]
}
```

### 5.2 新增

```http
POST /api/memory
{"channel_id": "room_123456", "user": "观众甲", "text": "我说过喜欢草莓", "reply": "记住啦", "weight": 1}
```

| 状态码 | 响应体 | 含义 |
| --- | --- | --- |
| 201 | `{"id": 3}` | 已写入并落盘 |
| 400 | `{"error":"empty text"}` | `text` 为空 |
| 503 | `{"error":"memory disabled"}` | 未配置记忆文件 |

### 5.3 删除

```http
DELETE /api/memory/3
```

写一条墓碑记录（文件只增不重写），读取与召回时过滤。

| 状态码 | 响应体 | 含义 |
| --- | --- | --- |
| 200 | `{"status":"deleted","id":3}` | 已标记删除 |
| 400 | `{"error":"invalid memory id"}` | ID 不是正整数 |
| 404 | `{"error":"记忆 3 不存在"}` | 没有这条记录（含重复删除） |

## 6. 工具

未开启 `[agent].enable_tools` 时返回 `503`。

### 6.1 列清单

```http
GET /api/tools
```

```json
{"count": 2, "tools": [{"name": "memory_search", "description": "...", "parameters": {"type": "object"}, "read_only": true}]}
```

`read_only` 由注册处决定：只有标记为只读的工具才允许直接调用（见下）。

### 6.2 直接调用

```http
POST /api/tools/memory_search
{"args": {"query": "草莓", "limit": 3}}
```

`args` 与模型给的参数同构，直接喂给工具处理器；执行结果原样返回。

| 状态码 | 响应体 | 含义 |
| --- | --- | --- |
| 200 | `{"name":"memory_search","result":"..."}` | 执行完成 |
| 403 | `{"error":"tool has side effects and is not directly callable"}` | 该工具没标记为只读 |
| 404 | `{"error":"unknown tool: x"}` | 工具未注册 |
| 500 | `{"error":"..."}` | 工具自身报错 |

**为什么有副作用就拒绝**：`/api/*` 不鉴权，把「写记忆」这类工具开给局域网等于把数据面也开了。
要放开得在注册处显式标记只读（`tool.Registry.RegisterReadOnly`），是个有意识动作。

## 7. 模型与表情

未配置 `[frontend]`（或没给模型目录）时返回 `503`。

### 7.1 当前模型与清单

```http
GET /api/models
```

```json
{
  "current": {"name": "mao_pro", "url": "/api/models/mao_pro/runtime/mao_pro.model3.json", "scale": 0.8, "expressions": ["F01"]},
  "models": [{"name": "mao_pro", "url": "...", "scale": 0.5}],
  "emotions": ["joy", "anger"]
}
```

`models` 是模型目录下扫描到的全部模型（不依赖 `model_dict.json`），`emotions` 是当前模型
`emotionMap` 的标签。

### 7.2 热切换模型

```http
POST /api/models/active
{"name": "hiyori"}
```

切换会重建模型信息与表情词表，并给所有已连接的浏览器补发 `hello`（页面据此换模型）。
会话侧取词表用的是函数而不是快照、Sink 在投递时才换算表情下标，所以切完两边都跟着换。

| 状态码 | 含义 |
| --- | --- |
| 200 | 已切换，响应带 `current` |
| 400 | 模型名为空 |
| 404 | 模型不在清单里 |
| 503 | 未启用 `[frontend]` |

### 7.3 表情预览

```http
POST /api/models/emotion
{"label": "joy"}
```

把标签换算成表达式下标后下发 `{"type":"emotion","data":{"label":"joy","emotion":3}}`，
页面只换表情、不出声。

| 状态码 | 含义 |
| --- | --- |
| 200 | 已下发，响应带 `emotion` 下标 |
| 400 | 标签为空或不在当前模型的词表里 |
| 503 | 未启用 `[frontend]` |

## 8. TTS 引擎与试听

未配置 `[tts].engines` 时试听返回 `503`；引擎清单接口始终可用（用来告诉前端「配了哪些」）。

### 8.1 引擎清单

```http
GET /api/tts
```

```json
{
  "count": 5,
  "engines": [
    {"name": "edge_tts", "enabled": false, "ready": true, "voice": "zh-CN-XiaoxiaoNeural"},
    {"name": "openai_tts", "enabled": true, "ready": true, "voice": "alloy", "model": "tts-1"}
  ]
}
```

`enabled` 表示它在 `[tts].engines` 的降级顺序里；`ready` 表示按当前配置能构造出引擎，
`false` 时 `reason` 写明原因（缺凭据、参数不齐等）。

### 8.2 试听

```http
POST /api/tts/preview
{"text": "你好呀", "engine": "openai_tts", "voice": "alloy"}
```

**直接返回 `audio/wav` 字节，不入播报队列**：试听要能挑任意引擎/音色，而队列里的音色由配置
决定。代价是前端播放可能与正在播的内容重叠出声——放不放、什么时候放由前端决定。
`engine` 省略时用 `[tts].engines` 的第一个；`voice` 省略时用该引擎配置里的音色。

| 状态码 | 含义 |
| --- | --- |
| 200 | 音频（WAV） |
| 400 | 文本为空 |
| 502 | 合成失败（引擎报错，响应体是原因） |
| 503 | 未配置 `[tts].engines` |

## 9. 推流

未启用 `[stream]` 时整组返回 `503`。

```http
GET /api/stream
```

```json
{"enabled": true, "running": false, "streaming": false, "output": "rtmp://live.example.com/app/stream?..."}
```

`running` 是推流循环在跑（可能在退避重试中），`streaming` 是 ffmpeg 真的推上了；
`output` 已去掉查询串里的 stream key，**接口不下发凭据**。开播被拒时带
`pending_code` / `pending_note`（如 60024 扫码、60043 实名、60045 账号准入）与只许本机访问的
`verify_url`。

```http
POST /api/stream
{"action": "start"}   # 或 "stop"
```

`start` 启动推流循环（`[stream].enabled` 时进程启动本来就会自动起）；`stop` 停循环并**等关播收尾**
（最长约 10 秒）——不等就等于把直播间挂成「直播中」。

| 状态码 | 含义 |
| --- | --- |
| 200 | 已执行，响应带最新 `stream` 状态 |
| 400 | `action` 不是 `start`/`stop` |
| 503 | 未启用 `[stream]` |

## 10. 路径迁移（已完成）

| 旧路径 | 现在 | 说明 |
| --- | --- | --- |
| `POST /inject` | `POST /api/speak` | 旧路径已下线 |
| `/web/` | `/` | 页面占根；`[[clients]].path` 也是 `/` 时按是否 WebSocket 升级分流 |
| `/client-ws` | `/api/client-ws` | |
| `/live2d-models/` | `/api/models/` | `/api/models/info` 返回模型清单 |
| `/login/*` | 不变 | 仅回环可访问 |

接入客户端的上报路径（`[[clients]].path`）是配置项，不受影响；把它配成 `/` 也仍然可用。
