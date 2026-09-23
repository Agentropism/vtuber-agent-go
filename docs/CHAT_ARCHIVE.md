# 对话归档

只增不删的完整对话与事件流水，按平台/渠道分类存储；重启后会话从这里恢复上下文。
HTTP 接口见 `docs/API.md` 第 11 节，调试台面板是 `/debug/#/chats`。

## 1. 定位：归档与记忆是两件事

| | 长期记忆（`memory` 包） | 对话归档（`archive` 包） |
| --- | --- | --- |
| 用途 | 关键词召回，供模型参考 | 完整流水，供人查看与导出 |
| 读写 | 可读可写可删（墓碑） | **只增不删** |
| 内容 | 一轮对话（用户 + 回复） | 对话 + 不产生回复的事件（点赞/进房/开播下播/通知） |
| 恢复上下文 | 不参与 | **参与**（rehydrate） |
| 文件 | `data/memory.jsonl` | `data/chats/<平台>/<渠道>.jsonl` |

## 2. 存储布局

```
data/chats/
  local/room_local.jsonl       本地文本框调试（保留渠道）
  qq/group_123456.jsonl        QQ 群
  qq/_notices.jsonl            渠道号为空的 QQ 通知
  bilibili/room_789.jsonl      B 站直播间
```

一行一条 JSON（JSON Lines），UTF-8；写入是「打开-追加-关闭」，文件只增不减。
单行损坏只跳过该行，不影响其余记录。

## 3. 记录格式

两类记录，字段如下（空的可选字段省略）：

```json
{"kind":"dialogue","time":"2026-09-21T23:00:00+08:00","platform":"qq","channel_id":"group_123456","channel_type":"group","user_id":"20002","user_name":"小明","event_kind":"group_message","text":"在吗","reply":"在的～"}
{"kind":"event","time":"2026-09-21T23:01:00+08:00","platform":"bilibili","channel_id":"room_789","user_id":"open_abc","user_name":"观众甲","event_kind":"like","text":"[点赞] x3"}
```

| 字段 | 说明 |
| --- | --- |
| `kind` | `dialogue`（有回复的对话）/ `event`（不产生回复的事件） |
| `time` | 事件发生时间；通知类事件（无时间戳）取落库时间 |
| `platform` | `qq` / `bilibili` / 自定义平台名 |
| `channel_id` | `group_123456` / `room_789` / `room_local` |
| `channel_type` | `group` / `live_room`（事件可能为空） |
| `user_id` / `user_name` | 发送者；`user_name` 为空时回退 `user_id` |
| `event_kind` | 事件种类，取值见 `docs/EVENT_CONTRACT.md` |
| `text` | 用户原文（礼物/SC 等是网关生成的描述文本） |
| `reply` | 仅 `dialogue`：Mili 的完整回复 |

## 4. 捕获点（保证不重不漏）

| 记录 | 写入位置 | 说明 |
| --- | --- | --- |
| `dialogue` | 会话层每轮结束（`session.turn` → `SessionsConfig.Archive`） | 弹幕/群消息/礼物/SC/大航海都走会话，带完整回复 |
| `event` | 上传管线终端包装器（`app.archiveUploadHandler`） | 用 `event.Kind.NeedsReply()` 分流：不产生回复的事件先归档再交给会话层；需要回复的由会话层连同回复归档，两边不重复 |

## 5. 分类规则

`archive.Classify(channelID, platform)`：

1. `room_local` 精确匹配 → `local`（本地调试的保留渠道）；
2. `group_` 前缀 → `qq`；
3. `room_` 前缀 → `bilibili`；
4. 其余用事件里的平台名，最后兜底 `other`。

分类目录与文件名都会做安全清洗（非字母数字/`_`/`-`/`.` 的字符替换为 `_`，首尾点号抹掉），
渠道名无法借 `../` 逃出归档目录。

## 6. 重启恢复（rehydrate）

配置 `[agent].history_rehydrate_turns`（0 = 关闭；本机配 12）：

- 渠道**首次创建会话**时，从归档读该渠道最近 N 轮 `dialogue`，构造
  `user`/`assistant` 历史灌给 `NewAgent`（`AgentConfig.InitialHistory`）；
- 只做历史，不触发回复；恢复的消息与正常轮次写进历史的内容完全同构，模型看到
  的上下文与进程没重启过一样；
- 没有回复的轮次只恢复用户消息，不造空助手消息；预置历史同样受
  `history_max_turns` / `history_max_tokens` 裁剪。

## 7. 配置

```toml
[agent]
archive_dir = "data/chats"          # 空 = 关闭归档（写入与恢复都关）
history_rehydrate_turns = 12        # 0 = 不恢复上下文
```

## 8. 实现位置

| 职责 | 文件 |
| --- | --- |
| 归档存储（分类/读写/导出） | `internal/core/agent/archive/archive.go` |
| 会话层归档与恢复接口 | `internal/core/agent/conversation/session.go` |
| 事件归档包装器与装配 | `internal/backend/app/archive.go` |
| HTTP 接口 | `internal/backend/api/chats.go` |
| 调试台面板 | `frontend/debug/src/views/ChatArchiveView.vue` |
