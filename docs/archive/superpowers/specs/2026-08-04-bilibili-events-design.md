# B站开放平台事件补全与礼物/舰长反馈 设计文档

- **日期**: 2026-08-04
- **对应 PRD**: 二、B站事件类型补全（扩展：礼物/SC/舰长反馈、舰长特殊支持）
- **复杂度**: 低
- **涉及项目**: `bilibili-live/`（Rust）、`onebot-gateway/`（Go）、`llm-vup-bridge/`（Go）
- **不涉及项目**: `Open-LLM-VTuber/`（本次零改动，两条消费链路均为其既有入口）

---

## 1. 背景与现状

### 1.1 现有事件链路

```
B站开放平台 WSS
  → bilibili-live (Rust)：msg_resp 用 serde 严格反序列化为 OpenPlatformPacket
  → WS 转发 {"cmd","data"} JSON 至 onebot-gateway（CONNECT 环境变量指定）
  → onebot-gateway (Go)：bilibililive.Dispatch 探测 cmd
      ├─ LIVE_OPEN_PLATFORM_DM   → 解码为 LiveOpenPlatformDMEvent → upload 至记忆服务
      │                             （Open-LLM-VTuber /proxy-ws → 注入活跃 session 对话）
      ├─ status                  → 调试日志
      └─ 其余全部 cmd            → "未知B站事件类型" 调试日志，丢弃
```

### 1.2 问题

1. **SC、点赞等事件在 Rust 侧反序列化失败（missing field 等 serde 错误）**，事件在 `msg_resp` 被整包丢弃，仅打日志。根因：`types.rs` 结构体把平台"条件性缺失/为 null/类型漂移"的字段声明成了必填。
2. **Go 网关只分发弹幕**，SC/礼物/舰长/点赞等事件即使到达网关也被丢弃。
3. **礼物/SC/舰长没有可靠的反馈来源**：`llm-vup-bridge/internal/dispatch/gift.go` 依赖上游投递 UnifiedEvent，但工作区内没有任何组件向 distillery 投递；GUARD（舰长）事件没有任何专属处理。
4. `distillery`（llm-vup-bridge 的事件消费端，`POST /event`）目前没有工作区内的上游接入。

### 1.3 已通过调研确认的具体结构体缺陷

对照 B站直播开放平台官方文档字段表，并用两个独立参考实现交叉验证
（Go SDK `shynome/openapi-bilibili`、`xfgryujk/blivechat` 前端解析）：

| # | 缺陷 | 后果 |
|---|------|------|
| 1 | `LiveOpenPlatformLiveRoomEnter` 误拷贝了 Like 的字段（`like_text`/`like_count`），ENTER 事件 payload 无此二字段 | 必现 `missing field like_text` |
| 2 | `LiveStart`/`LiveEnd` 把 `area_name`/`title` 声明为必填，参考实现字段表中不存在 | 开播/下播事件大概率 missing field |
| 3 | `like_count` 文档版本间存在 int / string 分歧（参考 Go SDK 按 string 解析） | `invalid type` 风险 |
| 4 | `union_id`、粉丝勋章字段、`blind_gift`、`combo_info` 等在特定条件下缺失或为 null，而 DM/礼物结构体将其声明为必填 | 条件性 missing field / invalid type: null |
| 5 | `OpenPlatformPacket` 为 serde 标签枚举，无未知 cmd 兜底；`data` 缺失时直接报 `missing field data` | 未知/异常推送静默丢弃 |

---

## 2. 需求确认结果（已与负责人逐项澄清）

| 问题 | 结论 |
|------|------|
| 改动范围 | **全链路三件套**：Rust 反序列化修复 + Go 网关全事件分发 + llm-vup-bridge 反馈完善 |
| 礼物反馈形式 | **取消价值分档**：礼物/SC/舰长各用一套固定模板；情绪/强度不做差异化，固定为现有 `joy/0.9`；优先级沿用现有消息类型映射（gift < SC < 舰长） |
| 舰长特殊支持 | GUARD 事件映射为 `captain` 类型：专属感谢模板 + 最高优先级（PriorityCaptain），无视冷却必回；**不区分**总督/提督/舰长等级 |
| 错误样本 | 无实际 payload 样本：**按官方文档 + 全面宽容化修复**，实播抓取真实 payload 存入 fixture 作为验收步骤 |
| 消费链路 | **双链路**：弹幕继续 upload→proxy-ws 进 LLM 对话（不动）；礼物/SC/舰长由网关转成 UnifiedEvent POST 给 distillery 做反馈，经 `/inject` 播报；点赞/进入/开播/下播仅上传记忆不发声 |
| Rust 修复方案 | **方案 A**：保留类型化结构体，字段宽容化（`#[serde(default)]`/`Option`），未知 cmd 透传不丢弃，fixture 回归测试 |

---

## 3. 目标架构（双链路）

```
B站开放平台 WSS
  │
  ▼
bilibili-live (Rust)  ── 宽容反序列化，任何事件都不再丢弃 ──▶ WS {"cmd","data"}
  │
  ▼
onebot-gateway (Go)  ── 按 cmd 分发，补全全部 11 类事件
  ├─ DM ───────────────▶ upload platformEvent ──▶ VTuber /proxy-ws（进 LLM 对话，原样不动）
  ├─ DM_MIRROR ────────▶ 仅日志（DM 镜像，避免重复上传）
  ├─ SEND_GIFT/SUPER_CHAT/GUARD
  │                    ├─▶ upload 记忆服务（platformEvent）
  │                    └─▶ POST UnifiedEvent ──▶ distillery:9528/event
  ├─ LIKE/LIVE_ROOM_ENTER/LIVE_START/LIVE_END ──▶ upload notice（不发声）
  └─ SUPER_CHAT_DEL/INTERACTION_END ──▶ 仅日志
                                      │
                                      ▼
              distillery (llm-vup-bridge)：按消息类型选模板 → 优先级调度
                                      │
                                      ▼
              POST /inject ──▶ Open-LLM-VTuber :12393（Live2D 出声 + 表情）
```

原则：
- 弹幕对话链路**零改动**。
- 反馈链路为**新增**：网关未配置 distillery 地址时退化为"仅上传记忆"，现有功能不受影响；distillery 宕机时网关仅记日志。

---

## 4. 序列化方式调研结论

1. **信封格式**：`{"cmd": "<事件名>", "data": {<事件体>}}`。Rust 侧现用 serde 标签枚举
   `#[serde(tag = "cmd", content = "data")]`，行为为：
   - `data` 中任一**必填字段缺失** → `missing field \`x\``，整包失败；
   - 字段值为 `null` 但类型非 `Option` → `invalid type: null`；
   - 字段 JSON 类型与声明不符（如 string vs int）→ `invalid type`；
   - 多余字段默认忽略（无害）；`cmd` 未知 → `unknown variant`。
2. **平台行为**：`union_id`、粉丝勋章、盲盒/连击信息等字段**条件性缺失或为 null**；
   文档版本间 `like_count` 类型存在分歧；`uid` 已废弃恒为 0 但仍下发。
3. **正确的序列化姿态**（本次落地标准）：
   - 字段名严格对齐官方文档 snake_case；
   - **所有事件字段一律 `#[serde(default)]`**，语义上可缺失的字段用 `Option<T>`；
   - 整型统一放宽为 `i64`（防溢出、兼容大小数值）；
   - `like_count` 用自定义反序列化器同时接受 int 与 string；
   - 枚举手写 `Deserialize`：先取 `{cmd, data: Value}`，按 cmd 路由解析；
     失败或未知 cmd 一律落入兜底变体并告警，**绝不静默丢事件**。
4. Go 侧 `encoding/json` 天然宽容（缺失字段取零值、多余字段忽略），按
   onebot-gateway 既有约定"可选字段用指针 + `omitempty`"补齐结构体即可。

---

## 5. 详细设计

### 5.1 bilibili-live（Rust）

**`src/types.rs`**：

1. 全部 11 个事件结构体按文档字段表修订，每个字段加 `#[serde(default)]`；
   `union_id`、`blind_gift`、`combo_info`、`anchor_info`（礼物）、`user_info`（GUARD）等
   条件性字段改为 `Option<T>`。
2. 修正 1.3 节所列缺陷：
   - `LiveRoomEnter` 移除 `like_text`/`like_count`，字段为
     `uname, uid, open_id, union_id?, uface, timestamp, room_id, msg_id`（全部 default）；
   - `LiveStart`/`LiveEnd` 的 `area_name`/`title` 改为 `Option<String>`（保留以兼容可能下发，缺失不报错），
     必备字段 `room_id, open_id, timestamp`；
   - `Like.like_count` 类型改为 `i64` + `#[serde(deserialize_with = "de_int_or_string_i64")]`；
   - `SuperChat.rmb` 保留 `f64`（serde_json 可将 JSON int 解入 f64）；
   - `Dm.union_id` 由 `String` 改 `Option<String>`（该字段条件性缺失；DM 目前可用只因线上恰好一直下发）。
3. `OpenPlatformPacket` 保留 `Serialize` derive（转发格式不变），**手写 `Deserialize`**：
   ```text
   {cmd: String, data: Value} →
     已知 cmd   → 对应结构体；解析失败 → warn 日志 + Unknown 兜底
     未知 cmd   → warn 日志 + Unknown { cmd, data }
     缺 data    → Unknown { cmd, data: Value::Null }
   ```
   新增兜底变体 `Unknown { cmd: String, data: serde_json::Value }`。
   `Status(bool)` 合成变体保持不变（bin.rs 依赖其序列化）。
4. `bin.rs` 的转发逻辑不动：`Unknown` 变体序列化为 `{"cmd":..., "data":...}` 原样透传给网关。

**`src/utils.rs`（或新文件 `src/de.rs`）**：新增 `de_int_or_string_i64`
（接受 `12`/`"12"`/缺失 → 0）兼容反序列化器，中文注释。

**测试**：`tests/fixtures/` 新增 13+ 个 JSON 夹具（按文档手工构造）：

- 11 类事件各 1 个"完整字段"样本；
- DM / SEND_GIFT / SUPER_CHAT / GUARD / LIKE 各 1 个"最小字段"样本（验证宽容化）；
- 1 个未知 cmd 样本、1 个缺 `data` 样本（验证 Unknown 兜底）。

单测：每个 fixture 解析到期望变体且关键字段值正确；未知样本落入 `Unknown`。
**验收补充**：实播联调时把抓到的真实 payload 存为 fixture 追加回归（写入交付说明）。

### 5.2 onebot-gateway（Go）

**`internal/event/bilibililive/message.go`**：新增事件结构体与 cmd 常量
（JSON tag 对齐文档 snake_case；可选字段指针 + omitempty；遵循包内既有风格）：

| Go 类型 | cmd |
|---------|-----|
| `LiveOpenPlatformSendGiftEvent`（含 AnchorInfo/ComboInfo/BlindGift 子结构） | `LIVE_OPEN_PLATFORM_SEND_GIFT` |
| `LiveOpenPlatformSuperChatEvent` | `LIVE_OPEN_PLATFORM_SUPER_CHAT` |
| `LiveOpenPlatformSuperChatDelEvent` | `LIVE_OPEN_PLATFORM_SUPER_CHAT_DEL` |
| `LiveOpenPlatformGuardEvent`（含 UserInfo 子结构） | `LIVE_OPEN_PLATFORM_GUARD` |
| `LiveOpenPlatformLikeEvent` | `LIVE_OPEN_PLATFORM_LIKE` |
| `LiveOpenPlatformLiveRoomEnterEvent` | `LIVE_OPEN_PLATFORM_LIVE_ROOM_ENTER` |
| `LiveOpenPlatformLiveStartEvent` / `LiveEndEvent` | `LIVE_OPEN_PLATFORM_LIVE_START` / `_LIVE_END` |
| `LiveOpenPlatformInteractionEndEvent` | `LIVE_OPEN_PLATFORM_INTERACTION_END` |
| `LiveOpenPlatformDMMirrorEvent` | `LIVE_OPEN_PLATFORM_DM_MIRROR` |

**`dispatch.go`**：`switch probe.Cmd` 补全上述 cmd，各走 `decodeAndDispatch`；
未知 cmd 维持 debug 日志。

**`handlers.go`**：为每个新事件类型建 `ActionList`（沿用 `newActionList`）。

**`app/register.go`**：`registerBilibili()` 扩展为路由表：

| 事件 | 动作 |
|------|------|
| DM | `UploadBilibiliDM`（不变） |
| DM_MIRROR | 仅调试日志（避免与 DM 重复） |
| SEND_GIFT / SUPER_CHAT / GUARD | ① upload 记忆服务 ② 转 UnifiedEvent POST distillery |
| LIKE / LIVE_ROOM_ENTER / LIVE_START / LIVE_END | upload notice（文本见下） |
| SUPER_CHAT_DEL / INTERACTION_END | 仅日志 |

**`internal/upload/client.go`**：新增通用构造函数
`buildBilibiliEventPlatformEvent(platform, channelID/room_id, user open_id/uname/uface, msg_id, timestamp, contentText)`。
另新增 `UploadBilibiliNotice(ctx, platform, openID, uname, text)`：B站用户标识是
`open_id` 字符串，现有 `UploadNotice(userID int64)` 签名不适用，需独立的字符串身份
构造路径（platformEvent 的 user_id/sender_id 直接填 open_id）。
上传文本格式：

| 事件 | content_text |
|------|--------------|
| SC | `[醒目留言 {rmb}元] {message}` |
| 礼物 | `[礼物] 赠送 {gift_name}×{gift_num}（价值 {r_price*gift_num/1000} 元）`；`paid=false` 时写（免费礼物） |
| GUARD | `[大航海] 开通{总督\|提督\|舰长}×{guard_num}{guard_unit}` |
| LIKE | `[点赞] {like_text}` |
| LIVE_ROOM_ENTER | `[进入直播间] {uname}` |
| LIVE_START / LIVE_END | `[开播]` / `[下播]` |

（注：上传文本里的价值/等级信息仅作记忆记录用途，不参与任何分档逻辑。）

**distillery 转发**：新增小模块（建议 `internal/distillery/`，避免污染 upload 包语义），
提供 `Init(targetURL)` 与 fire-and-forget `Post(event UnifiedEventJSON)`：
HTTP POST + 3s 超时 + 失败仅记日志（遵守 AGENTS.md"网络错误记录日志、不中断处理链"）。
配置：`config.toml` 新增

```toml
[bilibili]
distillery_target = "http://127.0.0.1:9528/event"   # 空字符串 = 不转发
```

**UnifiedEvent 映射**（网关侧构造，**不扩展 UnifiedEvent 现有字段**）：

```jsonc
{
  "platform": "bilibili",
  "user_id": "<open_id>",
  "user_name": "<uname>",
  "group_id": "room_<room_id>",
  "content": "<礼物：{gift_name}×{gift_num}；SC：留言正文；舰长：开通舰长×{guard_num}{guard_unit}>",
  "message_type": "gift" | "super_chat" | "captain"   // GUARD → captain
}
```

### 5.3 llm-vup-bridge（distillery）

改动收敛到**一个文件**：`internal/dispatch/gift.go`。

1. **删除按礼物名关键词猜档位的 `classifyGift`** 及其辅助函数
   （`contains`/`containsSubstr`）；礼物统一归入 `gift` 一档。
2. `giftTemplates` 收敛为三套固定模板（各 2–3 条，沿用用户名 hash 轮询选择）：

   | 类别 | 模板示例 |
   |------|----------|
   | `gift` | "谢谢{name}的礼物！么么哒~" / "收到{name}的礼物啦，开心！" |
   | `sc` | "感谢{name}的醒目留言！内容是：{content}"（SC 的 content 为留言正文，模板中带入） |
   | `captain` | "欢迎{name}上舰！以后就是一家人了！" / "{name}成为舰长了！感谢支持！" |

3. `BuildGiftReply` 签名与返回值不变（仍返回 string）；
   `message_type` 到类别的映射改为 `gift→gift、super_chat→sc、captain→captain`，
   其余返回空串（现状保持）。

**以下全部不动**（简化后现状即满足）：

- `internal/model/event.go`：UnifiedEvent 无需新增字段；
- `internal/dispatch/dispatcher.go`：优先级表（text < gift < SC < captain）与
  冷却/无视冷却逻辑已存在且符合要求；
- `cmd/distillery/main.go`：礼物/SC/舰长分支现状已走
  `BuildGiftReply` + 固定 `joy/0.9` + `ShouldRespond("gift_thanks", ...)`，即目标行为；
- `config.example.json`：无需新增配置项。

### 5.4 错误处理总则

- Rust：反序列化失败 → warn + Unknown 兜底；进程不退出（现状已满足，保留）。
- Go 网关：解码失败 → warn（现状）；distillery POST 失败 → warn，不重试不阻塞。
- distillery：未知 message_type → 现有 default 路径（PriorityLow），不 panic。
- 日志与注释全部中文（AGENTS.md 要求）。

---

## 6. 测试与验收

| 项目 | 命令 / 方式 |
|------|-------------|
| bilibili-live | `cargo fmt --check`、`cargo check`、`cargo test`、`cargo clippy --all-targets --all-features -- -D warnings`；fixture 全绿 |
| onebot-gateway | `go build ./cmd/onebot-gateway/`、`go vet ./...`、`go test ./...`；新增 dispatch 路由、上传文本构造、UnifiedEvent 映射单测 |
| llm-vup-bridge | `go build ./...`、`go test ./...`；gift.go 单测覆盖 gift/super_chat/captain 三类映射与未知类型返回空；`curl /test` 冒烟三种 type |
| 端到端（需真实直播，验收步骤） | ① bilibili-live 日志不再出现 missing field；② 网关日志可见各新 cmd 分发；③ 送礼物/SC/上舰时 VTuber 出声感谢，舰长可打断冷却；④ **抓取真实 payload 存入 `tests/fixtures/` 追加回归** |

---

## 7. 风险与开放项

1. **文档字段与真实推送仍有偏差的可能** —— 已由"全字段 default + Unknown 透传 + 实播 fixture 回补"三层兜底。
2. `like_count` 真实类型未最终确认 —— int_or_string 兼容反序列化覆盖两种情况。
3. distillery 成为网关的新运行时依赖 —— 未配置/宕机均只降级不致命。
4. `SC.rmb` 按文档为整型元，保留 f64 兼容浮点下发。
5. 工作区根目录非 git 仓库，本设计文档存于 `docs/superpowers/specs/`，不随子项目提交。
6. 礼物反馈为固定模板、固定情绪（joy/0.9），不按价值差异化 —— 负责人已确认的简化决策；后续如需恢复分档，只需扩展 gift.go 与 UnifiedEvent 字段。

## 8. 范围外（明确不做）

- PRD 一（自动重连）、四（TTS 排序/动作优先级调度）等其他条目；
- Open-LLM-VTuber Python 侧任何改动（两条链路均为其既有入口）；
- 弹幕对话链路（upload→proxy-ws）行为变更；
- **礼物价值分档、SC 金额分档、舰长等级区分、情绪/强度差异化**（已按负责人要求取消）。
