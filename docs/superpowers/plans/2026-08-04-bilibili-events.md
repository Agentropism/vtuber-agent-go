# B站开放平台事件补全 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 B站开放平台事件（SC/点赞等）反序列化 missing field 问题，补全全链路 11 类事件支持，并让礼物/SC/舰长事件经 distillery 获得专属语音反馈。

**Architecture:** 三仓库流水线改造：① `bilibili-live`（Rust）事件结构体全字段宽容化 + 未知事件兜底透传；② `onebot-gateway`（Go）补全全部 cmd 的分发，礼物/SC/舰长双写（记忆服务 + distillery），点赞/进入/开播/下播仅上传记忆；③ `llm-vup-bridge`（Go）gift.go 简化为按消息类型的固定模板反馈。弹幕对话链路（upload→proxy-ws）零改动，Open-LLM-VTuber 零改动。

**Tech Stack:** Rust 2024（serde/tokio/tracing）、Go（encoding/json、viper、zap）、JSON fixture 回归测试。

## Global Constraints

- 三个项目的**注释和日志一律使用中文**（各子项目 AGENTS.md 硬性要求）。
- 提交信息末尾追加 `Co-Authored-By: Claude <noreply@anthropic.com>`；仅暂存本任务涉及的文件；不提交 `config.toml`、密钥等私有配置。
- 各子项目是**独立 git 仓库**（`bilibili-live/`、`onebot-gateway/`、`llm-vup-bridge/`），在各自根目录执行 git。
- bilibili-live 验证命令：`cargo fmt --check`、`cargo check`、`cargo test`、`cargo clippy --all-targets --all-features -- -D warnings`。
- onebot-gateway / llm-vup-bridge 验证命令：`go build ./...`、`go vet ./...`、`go test ./...`。
- Go 约定：文件名全小写无下划线；导出标识符 PascalCase；JSON tag snake_case；网络/解码等可恢复错误记录日志、不中断处理链。
- 尽可能减少改动：不重构无关代码，不动弹幕对话链路，不动 Open-LLM-VTuber。
- 反馈策略（负责人已确认）：礼物/SC/舰长**各一套固定模板**，情绪固定 `joy/0.9`，不做价值分档、不做情绪差异化；优先级沿用现有映射（gift=3 < super_chat=4 < captain=5）。

**参考文档**：设计规格 `docs/superpowers/specs/2026-08-04-bilibili-events-design.md`（字段表与路由表的权威来源）。

---

## 文件结构总览

```
bilibili-live/                      （Rust，独立 git 仓库）
  src/de.rs                         新增：int/string 兼容反序列化器
  src/lib.rs                        修改：注册 mod de;
  src/types.rs                      重写：事件结构体宽容化 + 枚举手写 Serialize/Deserialize + Unknown 兜底
  tests/event_parse.rs              新增：fixture 回归测试
  tests/fixtures/*.json             新增：18 个事件夹具

onebot-gateway/                     （Go，独立 git 仓库）
  internal/event/bilibililive/message.go        修改：cmd 常量 + 10 个新事件结构体
  internal/event/bilibililive/dispatch.go       修改：补全 cmd 路由
  internal/event/bilibililive/handlers.go       修改：补全 ActionList
  internal/event/bilibililive/dispatch_test.go  新增：分发解码测试
  internal/upload/client.go                     修改：B站互动/通知上传构造器
  internal/upload/client_test.go                新增：上传构造测试
  internal/distillery/distillery.go             新增：distillery 转发包
  internal/distillery/distillery_test.go        新增：转发测试
  internal/app/register.go                      修改：B站事件路由注册
  internal/app/register_test.go                 新增：文本/事件构造测试
  internal/app/app.go                           修改：distillery 接线
  internal/config/config.go                     修改：[bilibili] 配置节
  config.toml.example                           修改：示例配置

llm-vup-bridge/                     （Go，独立 git 仓库）
  internal/dispatch/gift.go         重写：删除关键词分档，三套固定模板
  internal/dispatch/gift_test.go    新增：模板映射测试

docs/
  delivery-2026-08-04-bilibili-events.md        新增：交付说明（Task 9）
```

---

## Task 1: Rust 兼容反序列化器 de_int_or_string_i64

**Files:**
- Create: `bilibili-live/src/de.rs`
- Modify: `bilibili-live/src/lib.rs`（`mod` 声明区，约 12-17 行）

**Interfaces:**
- Produces: `pub fn de_int_or_string_i64<'de, D: Deserializer<'de>>(deserializer: D) -> Result<i64, D::Error>` —— Task 2 的 `LiveOpenPlatformLike.like_count` 字段会使用 `#[serde(deserialize_with = "de_int_or_string_i64")]` 引用它。

- [ ] **Step 1: 创建 de.rs，先写测试（此时函数未实现，编译应失败）**

创建 `bilibili-live/src/de.rs`：

```rust
//! 兼容反序列化器：处理 B站开放平台文档版本间类型不一致的字段。

use serde::{Deserialize, Deserializer};

/// 同时接受 JSON 整型、字符串型数值与 null 的 i64 反序列化器。
///
/// B站开放平台部分字段（如点赞事件的 like_count）在不同文档版本中
/// 类型标注不一致（int / string），线上推送可能两种形态都出现，
/// 此处统一归一化为 i64；null 视为 0。字段缺失时由 `#[serde(default)]` 处理。
pub fn de_int_or_string_i64<'de, D>(deserializer: D) -> Result<i64, D::Error>
where
    D: Deserializer<'de>,
{
    todo!()
}

#[cfg(test)]
mod tests {
    use super::de_int_or_string_i64;
    use serde::Deserialize;

    #[derive(Debug, Deserialize)]
    struct Sample {
        #[serde(default, deserialize_with = "de_int_or_string_i64")]
        like_count: i64,
    }

    /// 整型直接解析
    #[test]
    fn parses_int() {
        let s: Sample = serde_json::from_str(r#"{"like_count": 12}"#).unwrap();
        assert_eq!(s.like_count, 12);
    }

    /// 字符串型兼容解析
    #[test]
    fn parses_string() {
        let s: Sample = serde_json::from_str(r#"{"like_count": "34"}"#).unwrap();
        assert_eq!(s.like_count, 34);
    }

    /// null 归零
    #[test]
    fn null_becomes_zero() {
        let s: Sample = serde_json::from_str(r#"{"like_count": null}"#).unwrap();
        assert_eq!(s.like_count, 0);
    }

    /// 字段缺失取默认值 0
    #[test]
    fn missing_field_defaults_to_zero() {
        let s: Sample = serde_json::from_str(r#"{}"#).unwrap();
        assert_eq!(s.like_count, 0);
    }

    /// 非法内容应报错而非 panic
    #[test]
    fn invalid_content_errors() {
        assert!(serde_json::from_str::<Sample>(r#"{"like_count": "abc"}"#).is_err());
    }
}
```

- [ ] **Step 2: 在 lib.rs 注册模块**

在 `bilibili-live/src/lib.rs` 的 `mod config;` 之后插入一行：

```rust
mod de;
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd bilibili-live && cargo test de_int_or_string 2>&1 | tail -5`
Expected: 测试执行时 panic（`not yet implemented`），说明测试已挂到 todo 实现上。

- [ ] **Step 4: 实现反序列化器**

把 `de.rs` 中的 `todo!()` 替换为完整实现（函数体）：

```rust
pub fn de_int_or_string_i64<'de, D>(deserializer: D) -> Result<i64, D::Error>
where
    D: Deserializer<'de>,
{
    use serde::de::Error as _;

    let value = serde_json::Value::deserialize(deserializer)?;
    match value {
        serde_json::Value::Number(n) => n
            .as_i64()
            .ok_or_else(|| D::Error::custom(format!("数值超出 i64 范围: {n}"))),
        serde_json::Value::String(s) => s
            .trim()
            .parse::<i64>()
            .map_err(|err| D::Error::custom(format!("字符串无法解析为 i64: {s}: {err}"))),
        serde_json::Value::Null => Ok(0),
        other => Err(D::Error::custom(format!("无法解析为 i64: {other}"))),
    }
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd bilibili-live && cargo test de_int_or_string 2>&1 | tail -5`
Expected: 5 个测试全部 PASS。

- [ ] **Step 6: 格式化与静态检查**

Run: `cd bilibili-live && cargo fmt && cargo clippy --all-targets --all-features -- -D warnings 2>&1 | tail -3`
Expected: clippy 无告警。

- [ ] **Step 7: Commit**

```bash
cd bilibili-live
git add src/de.rs src/lib.rs
git commit -m "feat: 新增 int/string 兼容反序列化器，应对平台字段类型漂移

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 2: Rust 事件结构体宽容化 + fixture 回归测试

**Files:**
- Create: `bilibili-live/tests/fixtures/`（本任务 16 个 JSON 夹具）
- Create: `bilibili-live/tests/event_parse.rs`
- Modify: `bilibili-live/src/types.rs`（整体重写结构体部分；枚举 derive 本任务暂不动）

**Interfaces:**
- Consumes: Task 1 的 `crate::de::de_int_or_string_i64`
- Produces: 11 个宽容化的事件结构体（字段名不变、类型放宽、全部 `#[serde(default)]`）。Task 3 在此基础上手写枚举的 Serialize/Deserialize。

**背景**（实现者必读）：当前 `types.rs` 的缺陷——
1. `LiveOpenPlatformLiveRoomEnter` 误拷贝 Like 的 `like_text`/`like_count` 字段（ENTER payload 无此二字段）→ 必现 missing field；
2. `LiveStart`/`LiveEnd` 的 `area_name`/`title` 被声明为必填，参考实现字段表中不存在；
3. `Like.like_count` 存在 int/string 类型分歧；
4. `union_id`、`anchor_info`、`combo_info`、`blind_gift`、`user_info` 等条件性缺失字段被声明为必填。

- [ ] **Step 1: 创建夹具文件（16 个）**

在 `bilibili-live/tests/fixtures/` 下创建以下文件，内容原样照抄：

`dm_full.json`：
```json
{
  "cmd": "LIVE_OPEN_PLATFORM_DM",
  "data": {
    "uname": "测试用户",
    "uid": 0,
    "open_id": "open_id_1",
    "union_id": "union_id_1",
    "uface": "https://example.com/face.png",
    "timestamp": 1785800000,
    "room_id": 123456,
    "msg": "你好呀",
    "msg_id": "msg_001",
    "guard_level": 3,
    "fans_medal_wearing_status": true,
    "fans_medal_name": "小奖牌",
    "fans_medal_level": 5,
    "emoji_img_url": "",
    "dm_type": 0,
    "glory_level": 0,
    "reply_open_id": "",
    "reply_uname": "",
    "is_admin": 0
  }
}
```

`dm_minimal.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_DM", "data": {"msg": "只有正文"}}
```

`dm_mirror.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_DM_MIRROR", "data": {"timestamp": 1785800001, "room_id": 123456, "msg": "镜像弹幕", "msg_id": "msg_002", "emoji_img_url": "", "dm_type": 0}}
```

`gift_full.json`：
```json
{
  "cmd": "LIVE_OPEN_PLATFORM_SEND_GIFT",
  "data": {
    "room_id": 123456,
    "uid": 0,
    "open_id": "open_id_2",
    "union_id": "union_id_2",
    "uname": "送礼用户",
    "uface": "https://example.com/face2.png",
    "gift_id": 30607,
    "gift_name": "火箭",
    "gift_num": 1,
    "price": 500000,
    "r_price": 500000,
    "paid": true,
    "fans_medal_level": 10,
    "fans_medal_name": "奖牌",
    "fans_medal_wearing_status": true,
    "guard_level": 0,
    "timestamp": 1785800002,
    "anchor_info": {
      "uid": 0,
      "open_id": "anchor_open_id",
      "union_id": "",
      "uname": "主播",
      "uface": "https://example.com/anchor.png"
    },
    "msg_id": "msg_003",
    "gift_icon": "https://example.com/icon.png",
    "combo_gift": false,
    "combo_info": {
      "combo_base_num": 1,
      "combo_count": 1,
      "combo_id": "combo_1",
      "combo_timeout": 60
    },
    "blind_gift": {"blind_gift_id": 0, "status": false}
  }
}
```

`gift_minimal.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_SEND_GIFT", "data": {"gift_name": "小心心", "gift_num": 5, "paid": false}}
```

`super_chat_full.json`：
```json
{
  "cmd": "LIVE_OPEN_PLATFORM_SUPER_CHAT",
  "data": {
    "room_id": 123456,
    "uid": 0,
    "open_id": "open_id_3",
    "union_id": "union_id_3",
    "uname": "SC用户",
    "uface": "https://example.com/face3.png",
    "message_id": 9001,
    "message": "主播加油！",
    "msg_id": "msg_004",
    "rmb": 30,
    "timestamp": 1785800003,
    "start_time": 1785800003,
    "end_time": 1785800063,
    "guard_level": 0,
    "fans_medal_level": 0,
    "fans_medal_name": "",
    "fans_medal_wearing_status": false
  }
}
```

`super_chat_minimal.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_SUPER_CHAT", "data": {"message": "只有内容"}}
```

`super_chat_del.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_SUPER_CHAT_DEL", "data": {"room_id": 123456, "message_ids": [9001, 9002], "msg_id": "msg_005"}}
```

`guard_full.json`：
```json
{
  "cmd": "LIVE_OPEN_PLATFORM_GUARD",
  "data": {
    "user_info": {
      "uid": 0,
      "open_id": "open_id_4",
      "union_id": "union_id_4",
      "uname": "上舰用户",
      "uface": "https://example.com/face4.png"
    },
    "guard_level": 3,
    "guard_num": 1,
    "guard_unit": "月",
    "price": 198000,
    "fans_medal_level": 21,
    "fans_medal_name": "船票",
    "fans_medal_wearing_status": true,
    "timestamp": 1785800004,
    "room_id": 123456,
    "msg_id": "msg_006"
  }
}
```

`guard_minimal.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_GUARD", "data": {"guard_level": 3}}
```

`like_full.json`（**注意：like_count 为字符串**，验证兼容解析）：
```json
{
  "cmd": "LIVE_OPEN_PLATFORM_LIKE",
  "data": {
    "uname": "点赞用户",
    "uid": 0,
    "open_id": "open_id_5",
    "union_id": "union_id_5",
    "uface": "https://example.com/face5.png",
    "timestamp": 1785800005,
    "like_text": "点赞用户点赞了",
    "like_count": "12",
    "fans_medal_wearing_status": false,
    "fans_medal_name": "",
    "fans_medal_level": 0,
    "msg_id": "msg_007",
    "room_id": 123456
  }
}
```

`like_minimal.json`（like_count 为整型）：
```json
{"cmd": "LIVE_OPEN_PLATFORM_LIKE", "data": {"like_text": "某人点赞了", "like_count": 3}}
```

`enter.json`（**回归用例：无 like_text/like_count**）：
```json
{"cmd": "LIVE_OPEN_PLATFORM_LIVE_ROOM_ENTER", "data": {"uname": "进入用户", "uid": 0, "open_id": "open_id_6", "uface": "", "timestamp": 1785800006, "room_id": 123456, "msg_id": "msg_008"}}
```

`live_start.json`（**回归用例：无 area_name/title**）：
```json
{"cmd": "LIVE_OPEN_PLATFORM_LIVE_START", "data": {"open_id": "anchor_open_id", "room_id": 123456, "timestamp": 1785800007}}
```

`live_end.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_LIVE_END", "data": {"open_id": "anchor_open_id", "room_id": 123456, "timestamp": 1785800008}}
```

`interaction_end.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_INTERACTION_END", "data": {"game_id": "game_001", "timestamp": 1785800009}}
```

- [ ] **Step 2: 编写回归测试 tests/event_parse.rs**

创建 `bilibili-live/tests/event_parse.rs`：

```rust
//! 事件反序列化回归测试。
//!
//! 夹具位于 tests/fixtures/，初始按 B站直播开放平台官方文档字段表构造；
//! 实播联调抓到的真实 payload 应追加为新的夹具文件并补充断言。

use bilibili_live::types::OpenPlatformPacket;

fn parse(fixture: &str) -> OpenPlatformPacket {
    serde_json::from_str(fixture).expect("夹具应当反序列化成功")
}

#[test]
fn dm_full_fields() {
    match parse(include_str!("fixtures/dm_full.json")) {
        OpenPlatformPacket::Dm(dm) => {
            assert_eq!(dm.msg, "你好呀");
            assert_eq!(dm.uname, "测试用户");
            assert_eq!(dm.guard_level, 3);
            assert_eq!(dm.union_id.as_deref(), Some("union_id_1"));
        }
        other => panic!("期望 Dm 变体，实际 {other:?}"),
    }
}

#[test]
fn dm_minimal_fields_tolerant() {
    match parse(include_str!("fixtures/dm_minimal.json")) {
        OpenPlatformPacket::Dm(dm) => {
            assert_eq!(dm.msg, "只有正文");
            assert_eq!(dm.uname, "");
            assert_eq!(dm.union_id, None);
            assert_eq!(dm.room_id, 0);
        }
        other => panic!("期望 Dm 变体，实际 {other:?}"),
    }
}

#[test]
fn dm_mirror_fields() {
    match parse(include_str!("fixtures/dm_mirror.json")) {
        OpenPlatformPacket::DmMirror(mirror) => {
            assert_eq!(mirror.msg, "镜像弹幕");
            assert_eq!(mirror.room_id, 123456);
        }
        other => panic!("期望 DmMirror 变体，实际 {other:?}"),
    }
}

#[test]
fn gift_full_fields() {
    match parse(include_str!("fixtures/gift_full.json")) {
        OpenPlatformPacket::SendGift(gift) => {
            assert_eq!(gift.gift_name, "火箭");
            assert_eq!(gift.gift_num, 1);
            assert_eq!(gift.r_price, 500000);
            assert!(gift.paid);
            assert!(gift.anchor_info.is_some());
            assert_eq!(gift.anchor_info.unwrap().uname, "主播");
        }
        other => panic!("期望 SendGift 变体，实际 {other:?}"),
    }
}

#[test]
fn gift_minimal_fields_tolerant() {
    match parse(include_str!("fixtures/gift_minimal.json")) {
        OpenPlatformPacket::SendGift(gift) => {
            assert_eq!(gift.gift_name, "小心心");
            assert_eq!(gift.gift_num, 5);
            assert!(!gift.paid);
            assert!(gift.anchor_info.is_none());
            assert!(gift.combo_info.is_none());
            assert!(gift.blind_gift.is_none());
        }
        other => panic!("期望 SendGift 变体，实际 {other:?}"),
    }
}

#[test]
fn super_chat_full_fields() {
    match parse(include_str!("fixtures/super_chat_full.json")) {
        OpenPlatformPacket::SuperChat(sc) => {
            assert_eq!(sc.message, "主播加油！");
            assert_eq!(sc.message_id, 9001);
            assert_eq!(sc.rmb, 30.0);
        }
        other => panic!("期望 SuperChat 变体，实际 {other:?}"),
    }
}

#[test]
fn super_chat_minimal_fields_tolerant() {
    match parse(include_str!("fixtures/super_chat_minimal.json")) {
        OpenPlatformPacket::SuperChat(sc) => {
            assert_eq!(sc.message, "只有内容");
            assert_eq!(sc.uname, "");
        }
        other => panic!("期望 SuperChat 变体，实际 {other:?}"),
    }
}

#[test]
fn super_chat_del_fields() {
    match parse(include_str!("fixtures/super_chat_del.json")) {
        OpenPlatformPacket::SuperChatDel(del) => {
            assert_eq!(del.message_ids, vec![9001, 9002]);
        }
        other => panic!("期望 SuperChatDel 变体，实际 {other:?}"),
    }
}

#[test]
fn guard_full_fields() {
    match parse(include_str!("fixtures/guard_full.json")) {
        OpenPlatformPacket::Guard(guard) => {
            assert_eq!(guard.guard_level, 3);
            assert_eq!(guard.guard_unit, "月");
            let info = guard.user_info.expect("user_info 应存在");
            assert_eq!(info.uname, "上舰用户");
        }
        other => panic!("期望 Guard 变体，实际 {other:?}"),
    }
}

#[test]
fn guard_minimal_fields_tolerant() {
    match parse(include_str!("fixtures/guard_minimal.json")) {
        OpenPlatformPacket::Guard(guard) => {
            assert_eq!(guard.guard_level, 3);
            assert!(guard.user_info.is_none());
        }
        other => panic!("期望 Guard 变体，实际 {other:?}"),
    }
}

#[test]
fn like_string_count_compatible() {
    match parse(include_str!("fixtures/like_full.json")) {
        OpenPlatformPacket::Like(like) => {
            assert_eq!(like.like_count, 12);
            assert_eq!(like.like_text, "点赞用户点赞了");
        }
        other => panic!("期望 Like 变体，实际 {other:?}"),
    }
}

#[test]
fn like_int_count_compatible() {
    match parse(include_str!("fixtures/like_minimal.json")) {
        OpenPlatformPacket::Like(like) => assert_eq!(like.like_count, 3),
        other => panic!("期望 Like 变体，实际 {other:?}"),
    }
}

#[test]
fn live_room_enter_without_like_fields() {
    match parse(include_str!("fixtures/enter.json")) {
        OpenPlatformPacket::LiveRoomEnter(enter) => {
            assert_eq!(enter.uname, "进入用户");
            assert_eq!(enter.room_id, 123456);
        }
        other => panic!("期望 LiveRoomEnter 变体，实际 {other:?}"),
    }
}

#[test]
fn live_start_without_optional_fields() {
    match parse(include_str!("fixtures/live_start.json")) {
        OpenPlatformPacket::LiveStart(start) => {
            assert_eq!(start.room_id, 123456);
            assert_eq!(start.open_id, "anchor_open_id");
            assert_eq!(start.area_name, None);
            assert_eq!(start.title, None);
        }
        other => panic!("期望 LiveStart 变体，实际 {other:?}"),
    }
}

#[test]
fn live_end_fields() {
    match parse(include_str!("fixtures/live_end.json")) {
        OpenPlatformPacket::LiveEnd(end) => assert_eq!(end.room_id, 123456),
        other => panic!("期望 LiveEnd 变体，实际 {other:?}"),
    }
}

#[test]
fn interaction_end_fields() {
    match parse(include_str!("fixtures/interaction_end.json")) {
        OpenPlatformPacket::InteractionEnd(end) => assert_eq!(end.game_id, "game_001"),
        other => panic!("期望 InteractionEnd 变体，实际 {other:?}"),
    }
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd bilibili-live && cargo test --test event_parse 2>&1 | tail -8`
Expected: 多个测试 FAIL（`enter.json` 报 missing field `like_text`、`like_full.json` 报 invalid type、minimal 夹具报 missing field 等）。这正是待修复的线上缺陷。

- [ ] **Step 4: 重写 types.rs 的结构体部分**

用下面的完整内容**替换** `bilibili-live/src/types.rs` 中 `OpenPlatformPacket` 枚举**之后**的全部结构体定义（枚举本任务保持原样，Task 3 再改）。文件开头的 `use serde::{Deserialize, Serialize};` 改为：

```rust
use serde::{Deserialize, Serialize};

use crate::de::de_int_or_string_i64;
```

结构体完整内容如下（每个字段都带 `#[serde(default)]`，条件性字段为 `Option<T>`）：

```rust
/// 弹幕消息 LIVE_OPEN_PLATFORM_DM
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformDm {
    #[serde(default)]
    pub uname: String,
    /// 已废弃，平台固定下发 0
    #[serde(default)]
    pub uid: i64,
    #[serde(default)]
    pub open_id: String,
    /// 互通场景条件性下发，可能缺失
    #[serde(default)]
    pub union_id: Option<String>,
    #[serde(default)]
    pub uface: String,
    #[serde(default)]
    pub timestamp: i64,
    #[serde(default)]
    pub room_id: i64,
    #[serde(default)]
    pub msg: String,
    #[serde(default)]
    pub msg_id: String,
    #[serde(default)]
    pub guard_level: i64,
    #[serde(default)]
    pub fans_medal_wearing_status: bool,
    #[serde(default)]
    pub fans_medal_name: String,
    #[serde(default)]
    pub fans_medal_level: i64,
    #[serde(default)]
    pub emoji_img_url: String,
    #[serde(default)]
    pub dm_type: i64,
    #[serde(default)]
    pub glory_level: i64,
    #[serde(default)]
    pub reply_open_id: String,
    #[serde(default)]
    pub reply_uname: String,
    #[serde(default)]
    pub is_admin: i64,
}

/// 弹幕镜像消息 LIVE_OPEN_PLATFORM_DM_MIRROR
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformDmMirror {
    #[serde(default)]
    pub timestamp: i64,
    #[serde(default)]
    pub room_id: i64,
    #[serde(default)]
    pub msg: String,
    #[serde(default)]
    pub msg_id: String,
    #[serde(default)]
    pub emoji_img_url: String,
    #[serde(default)]
    pub dm_type: i64,
}

/// 礼物消息 LIVE_OPEN_PLATFORM_SEND_GIFT
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformSendGift {
    #[serde(default)]
    pub room_id: i64,
    #[serde(default)]
    pub uid: i64,
    #[serde(default)]
    pub open_id: String,
    #[serde(default)]
    pub union_id: Option<String>,
    #[serde(default)]
    pub uname: String,
    #[serde(default)]
    pub uface: String,
    #[serde(default)]
    pub gift_id: i64,
    #[serde(default)]
    pub gift_name: String,
    #[serde(default)]
    pub gift_num: i64,
    #[serde(default)]
    pub price: i64,
    /// 实际价值（金瓜子，1000 = 1 元）
    #[serde(default)]
    pub r_price: i64,
    #[serde(default)]
    pub paid: bool,
    #[serde(default)]
    pub fans_medal_level: i64,
    #[serde(default)]
    pub fans_medal_name: String,
    #[serde(default)]
    pub fans_medal_wearing_status: bool,
    #[serde(default)]
    pub guard_level: i64,
    #[serde(default)]
    pub timestamp: i64,
    /// 主播信息，条件性下发
    #[serde(default)]
    pub anchor_info: Option<SendGiftAnchorInfo>,
    #[serde(default)]
    pub msg_id: String,
    #[serde(default)]
    pub gift_icon: String,
    #[serde(default)]
    pub combo_gift: bool,
    /// 连击信息，非连击礼物可能缺失
    #[serde(default)]
    pub combo_info: Option<SendGiftComboInfo>,
    /// 盲盒信息，非盲盒礼物可能缺失
    #[serde(default)]
    pub blind_gift: Option<SendGiftBlindGift>,
}

/// 礼物事件中的主播信息
#[derive(Debug, Serialize, Deserialize)]
pub struct SendGiftAnchorInfo {
    #[serde(default)]
    pub uid: i64,
    #[serde(default)]
    pub open_id: String,
    #[serde(default)]
    pub union_id: Option<String>,
    #[serde(default)]
    pub uname: String,
    #[serde(default)]
    pub uface: String,
}

/// 礼物连击信息
#[derive(Debug, Serialize, Deserialize)]
pub struct SendGiftComboInfo {
    #[serde(default)]
    pub combo_base_num: i64,
    #[serde(default)]
    pub combo_count: i64,
    #[serde(default)]
    pub combo_id: String,
    #[serde(default)]
    pub combo_timeout: i64,
}

/// 盲盒信息
#[derive(Debug, Serialize, Deserialize)]
pub struct SendGiftBlindGift {
    #[serde(default)]
    pub blind_gift_id: i64,
    #[serde(default)]
    pub status: bool,
}

/// 醒目留言（SC）消息 LIVE_OPEN_PLATFORM_SUPER_CHAT
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformSuperChat {
    #[serde(default)]
    pub room_id: i64,
    #[serde(default)]
    pub uid: i64,
    #[serde(default)]
    pub open_id: String,
    #[serde(default)]
    pub union_id: Option<String>,
    #[serde(default)]
    pub uname: String,
    #[serde(default)]
    pub uface: String,
    #[serde(default)]
    pub message_id: i64,
    #[serde(default)]
    pub message: String,
    #[serde(default)]
    pub msg_id: String,
    /// 支付金额（元）；文档为整型，保留 f64 兼容
    #[serde(default)]
    pub rmb: f64,
    #[serde(default)]
    pub timestamp: i64,
    #[serde(default)]
    pub start_time: i64,
    #[serde(default)]
    pub end_time: i64,
    #[serde(default)]
    pub guard_level: i64,
    #[serde(default)]
    pub fans_medal_level: i64,
    #[serde(default)]
    pub fans_medal_name: String,
    #[serde(default)]
    pub fans_medal_wearing_status: bool,
}

/// SC 撤回消息 LIVE_OPEN_PLATFORM_SUPER_CHAT_DEL
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformSuperChatDel {
    #[serde(default)]
    pub room_id: i64,
    #[serde(default)]
    pub message_ids: Vec<i64>,
    #[serde(default)]
    pub msg_id: String,
}

/// 大航海（舰长）消息 LIVE_OPEN_PLATFORM_GUARD
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformGuard {
    /// 用户信息，条件性下发
    #[serde(default)]
    pub user_info: Option<GuardUserInfo>,
    /// 大航海等级：1=总督 2=提督 3=舰长
    #[serde(default)]
    pub guard_level: i64,
    #[serde(default)]
    pub guard_num: i64,
    /// 大航海单位（正常为"月"，其他内容以本字段为准）
    #[serde(default)]
    pub guard_unit: String,
    /// 大航海金瓜子
    #[serde(default)]
    pub price: i64,
    #[serde(default)]
    pub fans_medal_level: i64,
    #[serde(default)]
    pub fans_medal_name: String,
    #[serde(default)]
    pub fans_medal_wearing_status: bool,
    #[serde(default)]
    pub timestamp: i64,
    #[serde(default)]
    pub room_id: i64,
    #[serde(default)]
    pub msg_id: String,
}

/// 大航海事件中的用户信息
#[derive(Debug, Serialize, Deserialize)]
pub struct GuardUserInfo {
    #[serde(default)]
    pub uid: i64,
    #[serde(default)]
    pub open_id: String,
    #[serde(default)]
    pub union_id: Option<String>,
    #[serde(default)]
    pub uname: String,
    #[serde(default)]
    pub uface: String,
}

/// 点赞消息 LIVE_OPEN_PLATFORM_LIKE
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformLike {
    #[serde(default)]
    pub uname: String,
    #[serde(default)]
    pub uid: i64,
    #[serde(default)]
    pub open_id: String,
    #[serde(default)]
    pub union_id: Option<String>,
    #[serde(default)]
    pub uface: String,
    #[serde(default)]
    pub timestamp: i64,
    #[serde(default)]
    pub like_text: String,
    /// 对单个用户最近 2 秒点赞次数的聚合；文档版本间存在 int/string 分歧，做兼容解析
    #[serde(default, deserialize_with = "de_int_or_string_i64")]
    pub like_count: i64,
    #[serde(default)]
    pub fans_medal_wearing_status: bool,
    #[serde(default)]
    pub fans_medal_name: String,
    #[serde(default)]
    pub fans_medal_level: i64,
    #[serde(default)]
    pub msg_id: String,
    #[serde(default)]
    pub room_id: i64,
}

/// 进入直播间消息 LIVE_OPEN_PLATFORM_LIVE_ROOM_ENTER
///
/// 注意：payload 中**没有** like_text/like_count 字段（旧版结构体误拷贝自点赞事件，导致 missing field）。
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformLiveRoomEnter {
    #[serde(default)]
    pub uname: String,
    #[serde(default)]
    pub uid: i64,
    #[serde(default)]
    pub open_id: String,
    #[serde(default)]
    pub union_id: Option<String>,
    #[serde(default)]
    pub uface: String,
    #[serde(default)]
    pub timestamp: i64,
    #[serde(default)]
    pub room_id: i64,
    #[serde(default)]
    pub msg_id: String,
}

/// 开播消息 LIVE_OPEN_PLATFORM_LIVE_START
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformLiveStart {
    /// 分区名，可能缺失
    #[serde(default)]
    pub area_name: Option<String>,
    #[serde(default)]
    pub open_id: String,
    #[serde(default)]
    pub union_id: Option<String>,
    #[serde(default)]
    pub room_id: i64,
    #[serde(default)]
    pub timestamp: i64,
    /// 直播标题，可能缺失
    #[serde(default)]
    pub title: Option<String>,
}

/// 下播消息 LIVE_OPEN_PLATFORM_LIVE_END
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformLiveEnd {
    #[serde(default)]
    pub area_name: Option<String>,
    #[serde(default)]
    pub open_id: String,
    #[serde(default)]
    pub union_id: Option<String>,
    #[serde(default)]
    pub room_id: i64,
    #[serde(default)]
    pub timestamp: i64,
    #[serde(default)]
    pub title: Option<String>,
}

/// 互动结束消息 LIVE_OPEN_PLATFORM_INTERACTION_END
#[derive(Debug, Serialize, Deserialize)]
pub struct LiveOpenPlatformInteractionEnd {
    #[serde(default)]
    pub game_id: String,
    #[serde(default)]
    pub timestamp: i64,
}
```

注意：旧代码中 `LiveOpenPlatformDm.uid` 上的 `#[deprecated]` 属性删除（该属性会在使用处触发告警，字段保留但不再标注）。

- [ ] **Step 5: 运行测试确认全部通过**

Run: `cd bilibili-live && cargo test --test event_parse 2>&1 | tail -5`
Expected: 16 个测试全部 PASS。

- [ ] **Step 6: 全量检查**

Run: `cd bilibili-live && cargo fmt && cargo check 2>&1 | tail -3 && cargo test 2>&1 | tail -5 && cargo clippy --all-targets --all-features -- -D warnings 2>&1 | tail -3`
Expected: fmt 无差异、check 通过、全部测试通过、clippy 无告警。

- [ ] **Step 7: Commit**

```bash
cd bilibili-live
git add src/types.rs tests/event_parse.rs tests/fixtures/
git commit -m "fix: 宽容化 B站事件结构体字段，修复 SC/点赞等 missing field

- 全部事件字段改为 serde(default)，条件性字段改为 Option
- 修正 LiveRoomEnter 误拷贝点赞字段的问题
- LiveStart/LiveEnd 的 area_name/title 改为可选
- like_count 兼容 int/string 两种下发形态
- 新增 16 个 fixture 回归测试

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 3: Rust OpenPlatformPacket 手写 Serialize/Deserialize + Unknown 兜底

**Files:**
- Modify: `bilibili-live/src/types.rs`（枚举部分）
- Create: `bilibili-live/tests/fixtures/unknown_cmd.json`、`missing_data.json`
- Modify: `bilibili-live/tests/event_parse.rs`（追加 5 个测试）

**Interfaces:**
- Consumes: Task 2 的宽容化结构体
- Produces: `OpenPlatformPacket::Unknown { cmd: String, data: serde_json::Value }` 变体；对外序列化线格式不变（`{"cmd": "...", "data": {...}}`），Unknown 原样透传。`websocket.rs`（`serde_json::from_value::<OpenPlatformPacket>`）与 `bin.rs`（`serde_json::to_string(&oppacket)`）均不需改动。

- [ ] **Step 1: 新增 2 个兜底夹具**

`bilibili-live/tests/fixtures/unknown_cmd.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_FUTURE_EVENT", "data": {"some_field": 1}}
```

`bilibili-live/tests/fixtures/missing_data.json`：
```json
{"cmd": "LIVE_OPEN_PLATFORM_DM"}
```

- [ ] **Step 2: 在 tests/event_parse.rs 末尾追加测试**

```rust
#[test]
fn unknown_cmd_falls_back_to_unknown() {
    match parse(include_str!("fixtures/unknown_cmd.json")) {
        OpenPlatformPacket::Unknown { cmd, data } => {
            assert_eq!(cmd, "LIVE_OPEN_PLATFORM_FUTURE_EVENT");
            assert_eq!(data["some_field"], 1);
        }
        other => panic!("期望 Unknown 变体，实际 {other:?}"),
    }
}

#[test]
fn missing_data_falls_back_to_unknown() {
    match parse(include_str!("fixtures/missing_data.json")) {
        OpenPlatformPacket::Unknown { cmd, data } => {
            assert_eq!(cmd, "LIVE_OPEN_PLATFORM_DM");
            assert!(data.is_null());
        }
        other => panic!("期望 Unknown 变体，实际 {other:?}"),
    }
}

#[test]
fn unknown_variant_serializes_transparently() {
    let packet = parse(include_str!("fixtures/unknown_cmd.json"));
    let serialized: serde_json::Value = serde_json::to_value(&packet).unwrap();
    let original: serde_json::Value =
        serde_json::from_str(include_str!("fixtures/unknown_cmd.json")).unwrap();
    assert_eq!(serialized, original);
}

#[test]
fn status_serializes_as_before() {
    let value = serde_json::to_value(&OpenPlatformPacket::Status(true)).unwrap();
    assert_eq!(value["cmd"], "status");
    assert_eq!(value["data"], true);
}

#[test]
fn known_variant_keeps_wire_format() {
    let packet = parse(include_str!("fixtures/dm_full.json"));
    let value = serde_json::to_value(&packet).unwrap();
    assert_eq!(value["cmd"], "LIVE_OPEN_PLATFORM_DM");
    assert_eq!(value["data"]["msg"], "你好呀");
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd bilibili-live && cargo test --test event_parse 2>&1 | tail -10`
Expected: 编译失败（`Unknown` 变体不存在）——这就是本任务要实现的。

- [ ] **Step 4: 重写枚举（替换 types.rs 中的 OpenPlatformPacket 定义）**

把 `types.rs` 里原来的 `#[derive(Debug, Serialize, Deserialize)] #[serde(tag = "cmd", content = "data")] pub enum OpenPlatformPacket {...}` 整体替换为：

```rust
/// B站直播开放平台长链推送数据包。
///
/// 信封格式为 `{"cmd": "<事件名>", "data": {<事件体>}}`。
/// 反序列化为手写实现：先取 cmd 与 data 原始值，再按 cmd 路由解析为具体事件；
/// 解析失败或遇到未知 cmd 时落入 `Unknown` 兜底变体并记录告警，
/// 保证任何推送都不会被静默丢弃（Unknown 会被原样序列化透传给下游）。
#[derive(Debug)]
pub enum OpenPlatformPacket {
    /// 弹幕消息
    Dm(LiveOpenPlatformDm),
    /// 弹幕镜像消息
    DmMirror(LiveOpenPlatformDmMirror),
    /// 礼物消息
    SendGift(LiveOpenPlatformSendGift),
    /// 醒目留言（SC）消息
    SuperChat(LiveOpenPlatformSuperChat),
    /// SC 撤回消息
    SuperChatDel(LiveOpenPlatformSuperChatDel),
    /// 大航海（舰长）消息
    Guard(LiveOpenPlatformGuard),
    /// 点赞消息
    Like(LiveOpenPlatformLike),
    /// 进入直播间消息
    LiveRoomEnter(LiveOpenPlatformLiveRoomEnter),
    /// 开播消息
    LiveStart(LiveOpenPlatformLiveStart),
    /// 下播消息
    LiveEnd(LiveOpenPlatformLiveEnd),
    /// 互动结束消息
    InteractionEnd(LiveOpenPlatformInteractionEnd),
    /// 未知或解析失败的事件：保留原始 cmd 与 data，原样透传给下游
    Unknown { cmd: String, data: Value },
    /// 内部合成事件：连接状态（true = 已鉴权）
    Status(bool),
}

impl Serialize for OpenPlatformPacket {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        use serde::ser::{Error as _, SerializeMap};

        // Unknown 与 Status 直接按信封格式写出，不经过事件体序列化
        match self {
            OpenPlatformPacket::Unknown { cmd, data } => {
                let mut map = serializer.serialize_map(Some(2))?;
                map.serialize_entry("cmd", cmd)?;
                map.serialize_entry("data", data)?;
                return map.end();
            }
            OpenPlatformPacket::Status(status) => {
                let mut map = serializer.serialize_map(Some(2))?;
                map.serialize_entry("cmd", "status")?;
                map.serialize_entry("data", status)?;
                return map.end();
            }
            _ => {}
        }

        let (cmd, data) = match self {
            OpenPlatformPacket::Dm(event) => ("LIVE_OPEN_PLATFORM_DM", serde_json::to_value(event)),
            OpenPlatformPacket::DmMirror(event) => {
                ("LIVE_OPEN_PLATFORM_DM_MIRROR", serde_json::to_value(event))
            }
            OpenPlatformPacket::SendGift(event) => {
                ("LIVE_OPEN_PLATFORM_SEND_GIFT", serde_json::to_value(event))
            }
            OpenPlatformPacket::SuperChat(event) => {
                ("LIVE_OPEN_PLATFORM_SUPER_CHAT", serde_json::to_value(event))
            }
            OpenPlatformPacket::SuperChatDel(event) => {
                ("LIVE_OPEN_PLATFORM_SUPER_CHAT_DEL", serde_json::to_value(event))
            }
            OpenPlatformPacket::Guard(event) => {
                ("LIVE_OPEN_PLATFORM_GUARD", serde_json::to_value(event))
            }
            OpenPlatformPacket::Like(event) => ("LIVE_OPEN_PLATFORM_LIKE", serde_json::to_value(event)),
            OpenPlatformPacket::LiveRoomEnter(event) => {
                ("LIVE_OPEN_PLATFORM_LIVE_ROOM_ENTER", serde_json::to_value(event))
            }
            OpenPlatformPacket::LiveStart(event) => {
                ("LIVE_OPEN_PLATFORM_LIVE_START", serde_json::to_value(event))
            }
            OpenPlatformPacket::LiveEnd(event) => {
                ("LIVE_OPEN_PLATFORM_LIVE_END", serde_json::to_value(event))
            }
            OpenPlatformPacket::InteractionEnd(event) => {
                ("LIVE_OPEN_PLATFORM_INTERACTION_END", serde_json::to_value(event))
            }
            // Unknown 与 Status 已在上方提前返回
            OpenPlatformPacket::Unknown { .. } | OpenPlatformPacket::Status(_) => unreachable!(),
        };

        let data = data.map_err(S::Error::custom)?;
        let mut map = serializer.serialize_map(Some(2))?;
        map.serialize_entry("cmd", cmd)?;
        map.serialize_entry("data", &data)?;
        map.end()
    }
}

impl<'de> Deserialize<'de> for OpenPlatformPacket {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        /// 中间信封：cmd / data 均允许缺失
        #[derive(Deserialize)]
        struct Envelope {
            #[serde(default)]
            cmd: String,
            #[serde(default)]
            data: Value,
        }

        let Envelope { cmd, data } = Envelope::deserialize(deserializer)?;

        /// 尝试把 data 解析为具体事件；失败时记录告警，交由 Unknown 兜底
        fn try_parse<T: DeserializeOwned>(cmd: &str, data: &Value) -> Option<T> {
            match serde_json::from_value(data.clone()) {
                Ok(event) => Some(event),
                Err(err) => {
                    warn!("事件 {cmd} 反序列化失败，降级为透传: {err}");
                    None
                }
            }
        }

        let packet = match cmd.as_str() {
            "LIVE_OPEN_PLATFORM_DM" => try_parse(&cmd, &data).map(OpenPlatformPacket::Dm),
            "LIVE_OPEN_PLATFORM_DM_MIRROR" => {
                try_parse(&cmd, &data).map(OpenPlatformPacket::DmMirror)
            }
            "LIVE_OPEN_PLATFORM_SEND_GIFT" => {
                try_parse(&cmd, &data).map(OpenPlatformPacket::SendGift)
            }
            "LIVE_OPEN_PLATFORM_SUPER_CHAT" => {
                try_parse(&cmd, &data).map(OpenPlatformPacket::SuperChat)
            }
            "LIVE_OPEN_PLATFORM_SUPER_CHAT_DEL" => {
                try_parse(&cmd, &data).map(OpenPlatformPacket::SuperChatDel)
            }
            "LIVE_OPEN_PLATFORM_GUARD" => try_parse(&cmd, &data).map(OpenPlatformPacket::Guard),
            "LIVE_OPEN_PLATFORM_LIKE" => try_parse(&cmd, &data).map(OpenPlatformPacket::Like),
            "LIVE_OPEN_PLATFORM_LIVE_ROOM_ENTER" => {
                try_parse(&cmd, &data).map(OpenPlatformPacket::LiveRoomEnter)
            }
            "LIVE_OPEN_PLATFORM_LIVE_START" => {
                try_parse(&cmd, &data).map(OpenPlatformPacket::LiveStart)
            }
            "LIVE_OPEN_PLATFORM_LIVE_END" => try_parse(&cmd, &data).map(OpenPlatformPacket::LiveEnd),
            "LIVE_OPEN_PLATFORM_INTERACTION_END" => {
                try_parse(&cmd, &data).map(OpenPlatformPacket::InteractionEnd)
            }
            "status" => try_parse(&cmd, &data).map(OpenPlatformPacket::Status),
            _ => {
                warn!("未知B站事件类型，降级为透传: {cmd}");
                None
            }
        };

        Ok(packet.unwrap_or(OpenPlatformPacket::Unknown { cmd, data }))
    }
}
```

同时把 `types.rs` 文件头部的 use 语句更新为：

```rust
use serde::de::DeserializeOwned;
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use serde_json::Value;
use tracing::warn;

use crate::de::de_int_or_string_i64;
```

- [ ] **Step 5: 运行测试确认全部通过**

Run: `cd bilibili-live && cargo test --test event_parse 2>&1 | tail -5`
Expected: 21 个测试全部 PASS。

- [ ] **Step 6: 全量门禁（Phase 1 收尾）**

Run: `cd bilibili-live && cargo fmt && cargo check 2>&1 | tail -3 && cargo test 2>&1 | tail -6 && cargo clippy --all-targets --all-features -- -D warnings 2>&1 | tail -3`
Expected: 全部通过。若 `websocket.rs` 末尾的既有测试因枚举变更失败，检查其构造方式并修复（保持断言语义不变）。

- [ ] **Step 7: Commit**

```bash
cd bilibili-live
git add src/types.rs tests/event_parse.rs tests/fixtures/unknown_cmd.json tests/fixtures/missing_data.json
git commit -m "feat: OpenPlatformPacket 手写序列化，未知/异常事件透传不丢弃

- 反序列化按 cmd 路由解析，失败或未知 cmd 降级为 Unknown 兜底
- Unknown 变体序列化时原样保留 cmd/data，供下游网关透传
- 已知事件与 Status 的线格式保持不变

Co-Authored-By: Claude <noreply@anthropic.com>"
```
