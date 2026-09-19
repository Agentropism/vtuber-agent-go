# 事件契约文档（platformEvent）

## 1. 概述

网关把已处理的 QQ 群消息、QQ 通知和 B 站直播事件（弹幕、礼物、醒目留言、大航海等）统一封装成 **`platformEvent`**，交给本进程内的会话层消费。

事件不再出网：旧版经 WebSocket 上传给远端记忆服务，现在的终端是 `conversation.Sessions.Handle`，属于**进程内异步方法调用**。因此本文描述的是「网关内部的事件信封」，它同时也是对外契约——任何想接替会话层角色的实现，按这个结构解析即可。

相关文档：`docs/INJECT_API.md`（外部注入播报）、`docs/MEMORY_API.md`（记忆 HTTP 接口）、`docs/CONFIG_MIGRATION.md`（配置迁移）。

## 2. 生命周期与投递

| 项目 | 当前实现 |
| --- | --- |
| 终端 | `conversation.Sessions.Handle`，由 `app.Initialize()` 经 `upload.SetHandler` 注入 |
| 建立时机 | 应用启动时 `upload.Init(Options)` 调用一次，Options 接收缓存容量、等待上限、敏感词库、去重窗口等配置 |
| 投递方式 | 进程内方法调用，载荷是 `platformEvent` 的 JSON 字节；未注入处理器时事件被丢弃并记录日志 |
| 上传缓存 | 容量由 `[memory].queue_size` 配置（默认 256）的有界通道，满时阻塞入队（背压），等待上限由 `[memory].queue_wait_timeout` 配置（0=无限），超时丢弃并记录错误日志 |

### 事件管线

调用上传函数后，事件被序列化并进入管线，不等待处理结果。管线从前到后依次为：

1. **去重与敏感词过滤**（`internal/gateway/filter`）：先去重（键 `platform_name:channel_id:message_id`，`message_id` 为空时跳过；窗口由 `[memory].dedup_ttl` 配置，0=不启用），再对 `content_text` 做敏感词匹配（词库文件 `[memory].sensitive_words_file`，UTF-8 每行一词，`#` 开头为注释，前缀树匹配；文件缺失时 `upload.Init()` 返回错误并拒绝启动）。
2. **有界缓存背压**：入队时缓存已满则阻塞等待，等待上限由 `[memory].queue_wait_timeout` 配置（0=无限）；超时则丢弃该事件并记录错误日志。
3. **Action 顺序门控**：分发期间的上传请求暂存于 `DispatchScope`；server 把非零 Action 写回事件源客户端后、零 Action 事件在分发完成时，才放行入队，保证「先给出 Action，再上传」。
4. **串行消费**：专用消费协程逐个取出事件调用终端处理器，保证处理顺序与分发顺序一致。

## 3. 上行 JSON 结构

所有字段都会出现在 JSON 中；未填充的字符串为 `""`，未填充的数组为 `[]`，布尔值为 `false`，时间戳为 `0`。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `type` | string | 事件类别：`message` 或 `notice`。是否 `message` 等价于「是否需要回复」，是粗粒度判据 |
| `event_kind` | string | 事件种类，比 `type` 更细。见下表 |
| `platform_name` | string | 来源平台：当前为 `qq` 或 `bilibili` |
| `channel_id` | string | 会话/频道标识，例如 `group_10001`、`room_12345` |
| `channel_name` | string | 群名称或直播间名称 |
| `channel_type` | string | 渠道类型：`group` 或 `live_room` |
| `channel_avatar` | string | 渠道头像；当前始终为空字符串 |
| `user_id` | string | 用户标识 |
| `user_name` | string | 用户显示名称 |
| `user_avatar` | string | 用户头像 URL；仅 B 站弹幕会填入头像 |
| `message_id` | string | 消息标识；通知事件为空字符串 |
| `sender_id` | string | 发送者标识 |
| `sender_name` | string | 发送者显示名称 |
| `sender_nickname` | string | 原始昵称；QQ 群消息和 B 站弹幕会填充 |
| `sender_avatar` | string | 发送者头像 URL；仅 B 站弹幕会填入头像 |
| `content_data` | array | 内容段数组；当前只会发送一个文本段 |
| `content_data[].type` | string | 固定为 `text` |
| `content_data[].text` | string | 文本内容 |
| `content_text` | string | 文本内容，与文本段的 `text` 一致 |
| `is_tome` | boolean | 是否提及机器人；当前始终为 `false` |
| `timestamp` | integer | Unix 秒时间戳；通知事件当前为 `0` |
| `is_self` | boolean | 是否由机器人自身发出；QQ 群消息按 `self_id == user_id` 判断，其他事件为 `false` |
| `ref_chat_key` | string | 引用会话标识；当前为空字符串 |
| `ref_msg_id` | string | 引用消息标识；当前为空字符串 |
| `ref_sender_id` | string | 引用发送者标识；B 站弹幕填 `reply_open_id`，其他事件为空字符串 |

### 3.1 事件种类 `event_kind`

`type` 只区分 `message` / `notice`，粒度不足以让下游分辨来源——礼物、醒目留言、
大航海在协议上全部是 `message`。`event_kind` 给出更细的种类：

| `event_kind` | 来源 | 粗粒度 `type` | 是否需要回复 |
| --- | --- | --- | --- |
| `group_message` | QQ 群消息 | `message` | 是 |
| `danmaku` | B 站弹幕 | `message` | 是 |
| `super_chat` | B 站醒目留言 | `message` | 是 |
| `gift` | B 站礼物 | `message` | 是 |
| `guard` | B 站大航海 | `message` | 是 |
| `notice` | QQ 群通知 | `notice` | 否 |
| `like` | B 站点赞 | `notice` | 否 |
| `live_room_enter` | B 站进入直播间 | `notice` | 否 |
| `live_start` | B 站开播 | `notice` | 否 |
| `live_end` | B 站下播 | `notice` | 否 |

`type` 与 `event_kind` 由同一判据派生（是否需要回复），不会出现 `type: "message"`
却 `event_kind: "like"` 这类矛盾组合。下游决定播报优先级时使用 `event_kind`。

消费方遇到未声明的 `event_kind` 应当容错：按 `type` 的粗粒度判据处理即可，不要报错。

## 4. 消息事件

### 4.1 QQ 群消息

触发来源为 OneBot `message` / `group` 事件。`platform_name` 固定为 `qq`，`channel_id` 为 `group_<group_id>`，`channel_type` 为 `group`。

`user_name` 与 `sender_name` 依次取群名片 `sender.card`、昵称 `sender.nickname`、用户 ID 的字符串形式。`channel_name` 取 `group_name`；为空时使用 `group_<group_id>`。

```json
{
  "type": "message",
  "platform_name": "qq",
  "channel_id": "group_10001",
  "channel_name": "技术交流群",
  "channel_type": "group",
  "channel_avatar": "",
  "user_id": "20002",
  "user_name": "小明",
  "user_avatar": "",
  "message_id": "30003",
  "sender_id": "20002",
  "sender_name": "小明",
  "sender_nickname": "明",
  "sender_avatar": "",
  "content_data": [{"type": "text", "text": "大家好"}],
  "content_text": "大家好",
  "is_tome": false,
  "timestamp": 1719494400,
  "is_self": false,
  "ref_chat_key": "",
  "ref_msg_id": "",
  "ref_sender_id": ""
}
```

### 4.2 B 站直播弹幕

触发来源为 `LIVE_OPEN_PLATFORM_DM`。`platform_name` 固定为 `bilibili`，`channel_id` 为 `room_<room_id>`，`channel_name` 为 `直播间 <room_id>`，`channel_type` 为 `live_room`。

用户 ID 优先取 `open_id`，为空时取 `uid` 的字符串形式；用户名取 `uname`，为空时使用用户 ID。头像字段取 `uface`。

```json
{
  "type": "message",
  "platform_name": "bilibili",
  "channel_id": "room_12345",
  "channel_name": "直播间 12345",
  "channel_type": "live_room",
  "channel_avatar": "",
  "user_id": "open_id_abc",
  "user_name": "观众甲",
  "user_avatar": "https://example.com/avatar.jpg",
  "message_id": "msg_001",
  "sender_id": "open_id_abc",
  "sender_name": "观众甲",
  "sender_nickname": "观众甲",
  "sender_avatar": "https://example.com/avatar.jpg",
  "content_data": [{"type": "text", "text": "主播晚上好"}],
  "content_text": "主播晚上好",
  "is_tome": false,
  "timestamp": 1719494400,
  "is_self": false,
  "ref_chat_key": "",
  "ref_msg_id": "",
  "ref_sender_id": ""
}
```

## 5. 通知事件

QQ 的下列 OneBot 通知会被转换为 `type: "notice"` 后上传：

| OneBot 通知 | 上传文本含义 |
| --- | --- |
| `group_upload` | 群成员上传文件 |
| `group_admin` | 设置或取消管理员 |
| `group_decrease` | 退群或踢人 |
| `group_increase` | 入群或邀请入群 |
| `group_ban` | 禁言或解除禁言 |
| `friend_add` | 添加机器人为好友 |
| `group_recall` | 撤回群消息 |
| `friend_recall` | 撤回私聊消息 |
| `notify` / `poke` | 戳一戳 |
| `notify` / `lucky_king` | 红包运气王 |
| `notify` / `honor` | 获得群荣誉 |
| `notify` / `input_status` | 输入状态变化 |

通知不携带原始 OneBot JSON，而是上传网关生成的中文描述文本。其 `user_id`、`user_name`、`sender_id`、`sender_name` 均为被选定关联用户的 ID 字符串；频道、消息、头像、引用字段为空，`timestamp` 为 `0`。

```json
{
  "type": "notice",
  "platform_name": "qq",
  "channel_id": "",
  "channel_name": "",
  "channel_type": "",
  "channel_avatar": "",
  "user_id": "20002",
  "user_name": "20002",
  "user_avatar": "",
  "message_id": "",
  "sender_id": "20002",
  "sender_name": "20002",
  "sender_nickname": "",
  "sender_avatar": "",
  "content_data": [{"type": "text", "text": "QQ 20002 加入了群 10001"}],
  "content_text": "QQ 20002 加入了群 10001",
  "is_tome": false,
  "timestamp": 0,
  "is_self": false,
  "ref_chat_key": "",
  "ref_msg_id": "",
  "ref_sender_id": ""
}
```

## 6. 下行回复

会话生成回复文本后分两路：一路是文本下行（`ReplyFunc`），一路是语音播报（`broadcast.Queue`）。

文本下行的方向由 `internal/agent/conversation.buildReply` 决定：**目前只有 QQ 群有下行动作**（`send_group_msg`），B 站弹幕没有发送接口，因此只生成、不发送。

`server.SendAction` 把标准的 `action.Action` 写到**该事件来源平台**最新连接的客户端；目标平台取自事件本身（`platform_name`），不再依赖历史配置项 `[memory].callback_platform`。平台没有在线客户端、动作名称为空或写入失败时记录错误日志，且不重试。

## 7. 相关实现位置

| 职责 | 文件 |
| --- | --- |
| 事件信封组装、过滤、背压与投递 | `internal/gateway/upload/client.go` |
| 管线初始化 | `internal/app/app.go` |
| QQ 消息、通知及 B 站事件的注册 | `internal/app/register.go` |
| 事件种类契约 | `internal/shared/event/kind.go` |
| 事件终端（会话路由与回复） | `internal/agent/conversation/session.go` |
| 下行 Action 写回 | `internal/gateway/server/server.go` |
