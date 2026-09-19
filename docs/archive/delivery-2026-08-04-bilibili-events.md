# B站开放平台事件补全交付说明

- 实施日期：2026-08-05
- 对应设计：`docs/archive/superpowers/specs/2026-08-04-bilibili-events-design.md`（原 `docs/superpowers/…`，已归档）
- 实现仓库：`bilibili-live`、`onebot-gateway`
- 兼容验证：`llm-vup-bridge`（最终源码无净改动）
- 未修改：`Open-LLM-VTuber`
- 发布状态：`bilibili-live/dev@fcf5c09`、`onebot-gateway/main@2fa4354` 已推送；`llm-vup-bridge` 无任务提交

## 交付结果

### bilibili-live

- 11 类开放平台事件字段全部支持缺失值，条件性字段改为可选类型。
- `like_count` 同时接受 JSON 整数、字符串数值与 null。
- 修正进入直播间事件误用点赞字段，以及开播/下播可选字段导致的解析失败。
- `OpenPlatformPacket` 改为手写序列化与反序列化；未知 cmd、缺少 data 或已知事件解析失败时降级为 `Unknown`，原样透传 cmd/data。
- 新增 18 个 JSON fixture、21 个事件解析与线格式回归测试。

提交：

- `d2eb509` `feat: 新增 int/string 兼容反序列化器，应对平台字段类型漂移`
- `84376d3` `fix: 宽容化 B站事件结构体字段，修复 SC/点赞等 missing field`
- `00cb08e` `feat: OpenPlatformPacket 手写序列化，未知/异常事件透传不丢弃`
- `fcf5c09` `style: 应用 rustfmt 统一 Rust 源码格式`

### onebot-gateway

- 补全弹幕镜像、礼物、SC、SC 撤回、大航海、点赞、进入直播间、开播、下播和互动结束事件，总计支持 11 类 cmd。
- 礼物、SC、大航海同时上传记忆事件并异步转发 `UnifiedEvent` 到 distillery。
- 点赞、进入直播间、开播、下播使用 B站 `open_id` 字符串身份上传 notice。
- 弹幕镜像、SC 撤回和互动结束只保留调试日志，不重复上传或发声。
- distillery HTTP 客户端使用 3 秒超时；未配置、网络失败或非 2xx 响应只记录日志，不中断事件分发。
- 新增事件分发、上传构造、distillery 请求和注册映射测试。

提交：

- `36cab1f` `feat: 补全 B站开放平台事件分发`
- `feb9e0e` `feat: 新增 B站互动与通知上传构造`
- `2fa4354` `feat: 接通 B站高优先级事件语音反馈`

配置示例：

```toml
[bilibili]
# 空字符串表示禁用语音反馈转发
distillery_target = "http://127.0.0.1:9528/event"
```

实际私有 `config.toml` 未修改；部署时需按环境补充该配置并重启网关。

### llm-vup-bridge

- 最终源码与 `origin/main` 完全一致，不修改现有模板、价值分档、优先级和语音反馈行为。
- 兼容逻辑全部位于 `onebot-gateway`：礼物、SC、大航海分别映射为 bridge 已支持的 `gift`、`super_chat`、`captain`。
- 网关礼物 content 保留礼物名称与数量，可继续供 bridge 现有关键词分档逻辑使用。

## 验证结果

在各仓库根目录执行并通过：

```text
bilibili-live:
  cargo fmt --check
  cargo check
  cargo test
  cargo clippy --all-targets --all-features -- -D warnings

onebot-gateway:
  GOCACHE=/tmp/onebot-gateway-gocache go build ./...
  GOCACHE=/tmp/onebot-gateway-gocache go vet ./...
  GOCACHE=/tmp/onebot-gateway-gocache go test ./... -count=1

llm-vup-bridge:
  GOCACHE=/tmp/llm-vup-bridge-gocache go build ./...
  GOCACHE=/tmp/llm-vup-bridge-gocache go vet ./...
  GOCACHE=/tmp/llm-vup-bridge-gocache go test ./... -count=1
```

`llm-vup-bridge` 全量测试包含仓库原有、未跟踪的 `internal/tts/client_test.go` 本地监听用例，因此在允许绑定临时端口的环境中运行；该原有用例通过。兼容映射由 `onebot-gateway/internal/app/register_test.go` 覆盖。

## 尚需实播验收

- 使用真实 B站开放平台 WSS 推送验证 11 类事件；将抓到的真实 payload 去敏后追加到 `bilibili-live/tests/fixtures/`。
- 验证网关到实际记忆服务的 WebSocket 写入，以及记忆服务对 message/notice 的落库和权重行为。
- 启动 distillery 与 Open-LLM-VTuber，实测礼物、SC、舰长分别触发现有反馈、`joy/0.9` 和预期优先级，并确认 distillery 不可用时网关仍正常处理事件。
