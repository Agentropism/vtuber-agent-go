# B站高优先级事件语音反馈（distillery 转发）

## 1. 概述

本功能（提交 `2fa4354`，feat: 接通 B站高优先级事件语音反馈）为 B 站直播互动事件新增语音反馈通道：礼物、醒目留言和大航海属于高优先级互动，除继续上传记忆服务外，还会通过 HTTP 转发给 distillery 语音反馈服务（`llm-vup-bridge` 的 `/event` 端点）；点赞、进入直播间、开播和下播仅上传通知，不触发语音反馈。

转发为**可选**能力：`distillery_target` 配置为空字符串时完全禁用。转发采用异步 fire-and-forget 方式，网络失败只记录日志，不阻塞事件处理链。

## 2. 事件分类

| 协议命令（`cmd`） | 含义 | 记忆服务上传 | distillery 转发 | `message_type` |
| --- | --- | --- | --- | --- |
| `LIVE_OPEN_PLATFORM_DM` | 弹幕 | 弹幕（完整事件） | 否 | — |
| `LIVE_OPEN_PLATFORM_SEND_GIFT` | 礼物 | 礼物（完整事件） | 是 | `gift` |
| `LIVE_OPEN_PLATFORM_SUPER_CHAT` | 醒目留言 | 醒目留言（完整事件） | 是 | `super_chat` |
| `LIVE_OPEN_PLATFORM_GUARD` | 大航海 | 大航海（完整事件） | 是 | `captain` |
| `LIVE_OPEN_PLATFORM_LIKE` | 点赞 | 通知 | 否 | — |
| `LIVE_OPEN_PLATFORM_LIVE_ROOM_ENTER` | 进入直播间 | 通知 | 否 | — |
| `LIVE_OPEN_PLATFORM_LIVE_START` | 开播 | 通知 | 否 | — |
| `LIVE_OPEN_PLATFORM_LIVE_END` | 下播 | 通知 | 否 | — |

## 3. 配置

`config.toml` 新增可选配置段：

```toml
[bilibili]
# 礼物、醒目留言和大航海事件的语音反馈端点；空字符串表示禁用
distillery_target = "http://127.0.0.1:9528/event"
```

配置读取于启动时：`app.Initialize()` 调用 `distillery.SetLogger(log)` 与 `distillery.Init(cfg.Bilibili.DistilleryTarget)`。

## 4. distillery 转发协议

| 项目 | 当前实现 |
| --- | --- |
| 方法 | `POST` |
| 请求体 | JSON（`UnifiedEvent` 结构） |
| Content-Type | `application/json` |
| 请求超时 | 3 秒 |
| 发送方式 | 异步 goroutine，调用 `Post` 立即返回 |
| 失败处理 | 仅记录 `WARN` 日志，不重试、不阻塞事件处理链 |
| 非 2xx 响应 | 视为失败，记录日志 |
| 禁用状态 | `distillery_target` 为空时记录 `DEBUG` 日志并跳过 |

## 5. UnifiedEvent 结构

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `platform` | string | 固定为 `bilibili` |
| `user_id` | string | 用户标识：优先 `open_id`，为空时取 `uid` 字符串；两者皆空则为空字符串 |
| `user_name` | string | 用户显示名 `uname`；为空时回退为 `user_id` |
| `group_id` | string | 直播间标识 `room_<room_id>` |
| `content` | string | 事件内容文本（见下节构造明细） |
| `message_type` | string | 事件类别：`gift` / `super_chat` / `captain` |

示例（礼物事件）：

```json
{
  "platform": "bilibili",
  "user_id": "open_gift_abc",
  "user_name": "送礼用户",
  "group_id": "room_123456",
  "content": "火箭×2",
  "message_type": "gift"
}
```

## 6. 事件构造明细

| 事件 | `message_type` | `content` 构造 | 说明 |
| --- | --- | --- | --- |
| 礼物 | `gift` | `<gift_name>×<gift_num>`，如 `火箭×2` | 不携带礼物价格等额外字段 |
| 醒目留言 | `super_chat` | 留言原文 `message` | 不携带留言价格与排名 |
| 大航海 | `captain` | `开通舰长×<guard_num><guard_unit>`，如 `开通舰长×1月` | 舰长等级为 3 时 `GuardUnit` 为 `月`；用户信息取 `user_info` 子对象，缺失时用户字段为空 |

## 7. 通知事件文本

点赞、进入直播间、开播和下播经 `UploadBilibiliNotice` 走记忆服务的 notice 通道，上传网关生成的中文描述文本：

| 事件 | 上传文本 |
| --- | --- |
| 点赞 | `[点赞] <like_text>` |
| 进入直播间 | `[进入直播间] <uname>` |
| 开播 | `[开播]` |
| 下播 | `[下播]` |

点赞、进入直播间的关联用户按 `open_id` → `uid` 顺序取 `user_id`，`user_name` 为 `uname`；开播、下播事件两者均填 `open_id`。

## 8. 测试覆盖

| 测试 | 位置 | 覆盖点 |
| --- | --- | --- |
| `TestSendPostsUnifiedEvent` | `internal/distillery/distillery_test.go` | 请求方法、Content-Type、请求体 JSON 内容与 `UnifiedEvent` 一致 |
| `TestSendReportsNonSuccessStatus` | 同上 | 非 2xx 响应返回错误 |
| `TestBuildBilibiliGiftDistilleryEvent` | `internal/app/register_test.go` | 礼物事件平台、用户、`message_type` 与 content（`火箭×2`） |
| `TestBuildBilibiliSuperChatDistilleryEvent` | 同上 | 醒目留言 `message_type` 与原文 content |
| `TestBuildBilibiliGuardDistilleryEvent` | 同上 | 大航海用户字段与 content（`开通舰长×1月`） |
| `TestBuildBilibiliNoticeText` | 同上 | 点赞、进入直播间通知文本 |

## 9. 相关实现位置

| 职责 | 文件 |
| --- | --- |
| distillery HTTP 客户端、`UnifiedEvent` 定义与异步转发 | `internal/distillery/distillery.go` |
| B 站事件注册、事件构造与通知文本 | `internal/app/register.go` |
| distillery 初始化 | `internal/app/app.go` |
| `[bilibili].distillery_target` 配置 | `internal/config/config.go`、`config.toml.example` |
