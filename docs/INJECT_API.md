# 播报注入接口文档

## 1. 概述

> **端点为 `POST /api/speak`**（旧路径 `/inject` 已下线）。接口总览见 [`docs/API.md`](API.md)，
> 本文只讲播报注入的行为细节。

`POST /api/speak` 让外部程序把一段文本交给网关的统一播报队列：网关负责合成语音并按时序投递，前端负责出声、口型与表情。

它与平台事件是两条不同的入口：

| 入口 | 来源 | 说什么由谁决定 |
| --- | --- | --- |
| `upload` 管线（弹幕、礼物、SC、群消息） | 平台客户端上报 | agent（LLM 结合会话上下文生成） |
| `POST /api/speak` | 外部程序主动调用 | 调用方，文本原样播报，不经过 LLM |

**入队即返回**：响应 `200` 只表示已进入播报队列，不代表已经合成完、更不代表已经播完。播报的排队、抢占、冷却与并行合成统一由 `internal/core/agent/broadcast` 负责，因此事件回复与注入内容不会同时出声。

## 2. 请求

```http
POST /api/speak HTTP/1.1
Content-Type: application/json

{"text": "要说的话", "emotion": "joy"}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `text` | string | 是 | 要播报的文本；裁剪首尾空白后不能为空 |
| `emotion` | string | 否 | Live2D 表情标签（模型 `emo_map` 的英文名，如 `joy`）。空表示不换表情。取值是否可用由前端加载的模型决定，网关不校验、原样透传 |

请求体上限 64 KiB。

示例：

```bash
curl -X POST localhost:6199/api/speak \
  -H 'Content-Type: application/json' \
  -d '{"text":"这是注入的测试播报","emotion":"joy"}'
```

## 3. 响应

| 状态码 | 响应体 | 含义 |
| --- | --- | --- |
| 200 | `{"status":"ok"}` | 已进入播报队列 |
| 400 | `{"error":"empty text"}` | `text` 为空或只有空白 |
| 400 | `{"error":"invalid json body"}` | 请求体不是合法 JSON |
| 405 | `{"error":"method not allowed"}` | 使用了 POST 以外的方法 |
| 503 | `{"error":"broadcast disabled"}` | 未配置 `[tts].engines`，播报链路没有装配 |
| 503 | `{"error":"..."}` | 队列已满，该条目因优先级最低被丢弃 |

## 4. 播报优先级

注入条目以 `proactive`（LLM 主动互动档）入队，低于弹幕、礼物与醒目留言，高于待机发言。理由是注入不是观众带来的事件，属于「非事件驱动的话」；代价是弹幕密集时注入会被压后。

分级与实现位于 `internal/core/agent/broadcast`：`PriorityFor` 负责事件到优先级的映射，注入的取值在 `app` 装配处指定。

## 5. 表情的落地路径

`emotion` 随播报条目（`broadcast.Item.Emotion`）流经队列，由 `Sink` 在投递时按当前模型的 `emotionMap` 换算成 Live2D 表达式下标，经 `/api/client-ws` 的 `speak` 消息带给浏览器（见 `internal/backend/web`）。未配置 `[frontend]` 时退回只记日志的占位实现，表情不生效。
