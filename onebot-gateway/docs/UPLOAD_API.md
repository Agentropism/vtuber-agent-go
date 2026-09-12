# 上传信息接口文档

## 1. 概述

onebot-gateway 将已处理的 QQ 群消息、QQ 通知和 B 站直播弹幕异步上传到外部记忆服务。上传使用 WebSocket 文本帧，单帧内容为一个 JSON 对象。

本文描述网关主动发送给记忆服务的协议；`docs/MEMORY_API.md` 中的 HTTP 接口不属于本上传通道。

## 2. 连接与生命周期

| 项目 | 当前实现 |
| --- | --- |
| 目标地址 | `config.toml` 的 `[memory].target`，例如 `ws://127.0.0.1:12393/proxy-ws` |
| 回调目标平台 | `config.toml` 的 `[memory].callback_platform`，例如 `qq` |
| 建立时机 | 应用启动时，`app.Initialize()` 调用一次 `upload.Init(target, callbackPlatform, Options)`，Options 接收缓存容量、等待上限、敏感词库、去重窗口等配置 |
| 帧类型 | WebSocket 文本帧 |
| 连接超时 | 10 秒 |
| 压缩 | 禁用 WebSocket 压缩 |
| 初次连接失败重试 | 间隔 3 秒 |
| 已连接后断开重连 | 间隔 1 秒 |
| 上传缓存 | 容量由 `[memory].queue_size` 配置（默认 256）的有界通道，满时阻塞入队（背压），等待上限由 `[memory].queue_wait_timeout` 配置（0=无限），超时丢弃并记录错误日志 |

### 上传管线

调用上传函数后，事件被序列化并进入上传管线，不会等待远端响应。管线从前到后依次为：

1. **去重与敏感词过滤**（`gateway/filter`）：先去重（键 `platform_name:channel_id:message_id`，`message_id` 为空时跳过；窗口由 `[memory].dedup_ttl` 配置，0=不启用），再对 `content_text` 做敏感词匹配（词库文件 `[memory].sensitive_words_file`，UTF-8 每行一词，`#` 开头为注释，前缀树匹配；文件缺失时 `upload.Init()` 返回错误并拒绝启动）。
2. **有界缓存背压**：入队时缓存已满则阻塞等待，等待上限由 `[memory].queue_wait_timeout` 配置（0=无限）；超时则丢弃该事件并记录错误日志。
3. **Action 顺序门控**：分发期间的上传请求暂存于 `DispatchScope`；server 把非零 Action 写回事件源客户端后、零 Action 事件在分发完成时，才放行入队，保证「先给出 Action，再上传」。
4. **远程写锁**：发送 worker 获取写锁 `writeMu` 后才写远程 WebSocket，同一时刻只有一个在途上传。

写入 WebSocket 失败时，该帧不会重新放回队列，连接随后重建。目标地址（`[memory].target`）为空时，`upload.Init()` 仅记录错误并跳过初始化，上传被直接丢弃。

## 3. 上行 JSON 结构

所有字段都会出现在 JSON 中；未填充的字符串为 `""`，未填充的数组为 `[]`，布尔值为 `false`，时间戳为 `0`。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `type` | string | 事件类别：`message` 或 `notice` |
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

## 6. 下行回调

记忆服务可在同一 WebSocket 连接中向网关发送 JSON 格式的标准 `action.Action`。网关会记录动作名称、参数、回显和目标平台，再原样转发给 `[memory].callback_platform` 对应平台最新连接的客户端。

回调负载不增加 `platform` 等包装字段；目标由网关配置确定。回调目标平台未配置、该平台没有在线客户端、动作名称为空或客户端写入失败时，网关会记录错误日志，且不会重试该动作。

## 7. 相关实现位置

| 职责 | 文件 |
| --- | --- |
| WebSocket 连接、队列和 JSON 组装 | `gateway/upload/client.go` |
| 上传初始化 | `app/app.go` |
| QQ 消息、通知及 B 站弹幕的注册 | `app/register.go` |
| 目标地址配置 | `config.toml` 的 `[memory].target` |
