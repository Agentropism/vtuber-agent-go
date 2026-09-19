# vtuber-agent-go 对接文档

## 概述

vtuber-agent-go 是一个单二进制多平台接入服务，接收客户端上报的 OneBot 格式事件，分发到处理器链并上传到记忆服务。

## 连接方式

客户端通过 WebSocket 连接网关，路径在 `config.toml` 的 `[[clients]]` 中配置：

```toml
[[clients]]
platform = "qq"
adapter_key = "onebot_v11"
path = "/qq"

[[clients]]
platform = "bilibili"
adapter_key = "bilibili_live"
path = "/bilibili"
```

- QQ 端连接：`ws://地址:端口/qq`
- B站 端连接：`ws://地址:端口/bilibili`

## 消息格式

所有消息均为 JSON 文本帧，字段命名遵循 OneBot v11 snake_case 规范。

### 通用字段

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `time` | int64 | 是 | 事件时间戳（Unix 秒） |
| `self_id` | int64 | 是 | 机器人自身 ID（B站 填 0 即可） |
| `post_type` | string | 是 | 事件类型：`message` / `notice` / `request` / `meta_event` |

---

## 事件类型详情

### 1. 群消息 (message group)

最常用的事件，弹幕/聊天发言均走此类型。

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "message",
  "message_type": "group",
  "sub_type": "normal",
  "message_id": 1,
  "group_id": 123456,
  "group_name": "直播间名或群名",
  "user_id": 789,
  "raw_message": "弹幕或消息文本",
  "sender": {
    "user_id": 789,
    "nickname": "用户昵称",
    "card": ""
  }
}
```

| 字段 | 说明 |
| --- | --- |
| `message_type` | 固定 `"group"` |
| `group_id` | 群号 / 直播间号 |
| `group_name` | 群名 / 主播名（可为空） |
| `raw_message` | 消息原文（纯文本） |
| `sender.nickname` | 发送者昵称 |
| `sender.card` | 群名片（无则空字符串） |

B站 映射：
- `group_id` → 直播间号
- `group_name` → 主播名
- `raw_message` → 弹幕内容 / 礼物格式化文本

---

### 2. 群文件上传 (notice group_upload)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "group_upload",
  "group_id": 1003245823,
  "user_id": 3221834658,
  "file": { "id": "xxx", "name": "图片.jpg", "size": 102400, "busid": 1 }
}
```

---

### 3. 群管理员变更 (notice group_admin)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "group_admin",
  "sub_type": "set",
  "group_id": 1003245823,
  "user_id": 3221834658
}
```

`sub_type`：`"set"` 设为管理 / `"unset"` 取消管理

---

### 4. 群成员减少 (notice group_decrease)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "group_decrease",
  "sub_type": "leave",
  "group_id": 1003245823,
  "user_id": 3945735428,
  "operator_id": 3221834658
}
```

`sub_type`：`"leave"` 主动退出 / `"kick"` 被踢 / `"kick_me"` 机器人被踢

---

### 5. 群成员增加 (notice group_increase)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "group_increase",
  "sub_type": "invite",
  "group_id": 1003245823,
  "user_id": 3945735428,
  "operator_id": 3221834658
}
```

`sub_type`：`"invite"` 被邀请 / `"approve"` 管理员同意

B站 映射：
- `sub_type: "approve"` → 用户进入直播间

---

### 6. 群禁言 (notice group_ban)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "group_ban",
  "sub_type": "ban",
  "group_id": 1003245823,
  "user_id": 3945735428,
  "operator_id": 3221834658,
  "duration": 600
}
```

`sub_type`：`"ban"` 禁言 / `"lift_ban"` 解除禁言

---

### 7. 好友添加 (notice friend_add)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "friend_add",
  "user_id": 3945735428
}
```

---

### 8. 群消息撤回 (notice group_recall)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "group_recall",
  "group_id": 1003245823,
  "user_id": 3945735428,
  "operator_id": 3221834658,
  "message_id": 12345
}
```

---

### 9. 私聊消息撤回 (notice friend_recall)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "friend_recall",
  "user_id": 3945735428,
  "message_id": 12345
}
```

---

### 10. 戳一戳 (notice notify poke)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "notify",
  "sub_type": "poke",
  "group_id": 1003245823,
  "user_id": 3221834658,
  "target_id": 3945735428
}
```

---

### 11. 运气王 (notice notify lucky_king)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "notify",
  "sub_type": "lucky_king",
  "group_id": 1003245823,
  "user_id": 3221834658,
  "target_id": 3945735428
}
```

---

### 12. 群荣誉 (notice notify honor)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "notify",
  "sub_type": "honor",
  "group_id": 1003245823,
  "user_id": 3221834658,
  "honor_type": "talkative"
}
```

`honor_type`：`"talkative"` 龙王 / `"performer"` 群聊之火 / `"emotion"` 快乐源泉

---

### 13. 输入状态 (notice notify input_status)

NapCat 扩展事件。

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "notice",
  "notice_type": "notify",
  "sub_type": "input_status",
  "group_id": 0,
  "user_id": 3221834658,
  "status_text": "对方正在输入...",
  "event_type": 1
}
```

---

### 14. 请求 (request friend / group)

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "request",
  "request_type": "friend",
  "user_id": 3945735428,
  "comment": "验证消息",
  "flag": "请求标识"
}
```

`request_type`：`"friend"` 好友请求 / `"group"` 加群请求

---

### 15. 元事件 (meta_event)

心跳和生命周期事件，客户端必须定期发送。

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "meta_event",
  "meta_event_type": "heartbeat",
  "status": { "online": true, "good": true },
  "interval": 30000
}
```

```json
{
  "time": 1719494400,
  "self_id": 0,
  "post_type": "meta_event",
  "meta_event_type": "lifecycle",
  "sub_type": "connect"
}
```

---

## 响应

网关对**非零 Action** 返回 JSON 响应：

```json
{
  "action": "send_group_msg",
  "params": {
    "group_id": 1003245823,
    "message": "回复内容"
  }
}
```

如果事件没有被任何 handler 产生非零 action，则无响应。

---

## B站 Live 对接快速参考

B站 直播事件转 OneBot 格式的推荐映射：

| B站 事件 | `post_type` | `message_type`/`notice_type` | 关键字段映射 |
| --- | --- | --- | --- |
| 弹幕 | `message` | `group` | `group_id`=直播间号, `raw_message`=弹幕文本 |
| 礼物 | `message` | `group` | `raw_message`="送了 x 个 y" |
| 进入直播间 | `notice` | `group_increase` | `sub_type`="approve" |
| 关注主播 | `notice` | `group_increase` | `sub_type`="approve" |
| 上舰 | `notice` | `group_increase` | `sub_type`="approve" |
| 续费/开通 🛡️ | `notice` | `group_increase` | `sub_type`="approve" |

`group_id` 使用直播间号，`group_name` 使用主播名，`sender.nickname` 使用用户昵称。
