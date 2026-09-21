#!/usr/bin/env bash
#
# 端到端冒烟：真二进制 + 假 LLM + 假 TTS + 真 WebSocket 客户端。
#
# 验证的是完整链路：B 站弹幕 → 分渠道会话 → LLM 流式回复 → 逐句入播报队列 →
# TTS 合成 → 前端出声。全部在临时目录里跑，结束后自动清理，不碰真实配置。
#
# 用法：
#   scripts/e2e/run.sh                     # 用默认端口
#   SMOKE_MODELS_DIR=/path/to/live2d-models scripts/e2e/run.sh   # 指定模型目录（可选）
#
# 模型目录存在时会一并启用前端接入，否则只验证到播报队列（用占位 Sink）。

set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORK_DIR="$(mktemp -d /tmp/syagent-smoke.XXXXXX)"

GW_PORT="${SMOKE_GW_PORT:-16199}"
LLM_PORT="${SMOKE_LLM_PORT:-19401}"
TTS_PORT="${SMOKE_TTS_PORT:-19402}"
MODELS_DIR="${SMOKE_MODELS_DIR:-$REPO_DIR/../Open-LLM-VTuber/live2d-models}"
MODEL_DICT="${SMOKE_MODEL_DICT:-$REPO_DIR/../Open-LLM-VTuber/model_dict.json}"

cleanup() {
    # 只杀本脚本起的两类进程，且用绝对路径锚定，避免误伤调用方的 shell
    pkill -f "^$WORK_DIR/vtuber-agent-go" 2>/dev/null || true
    pkill -f "^python3 $WORK_DIR/fakes.py" 2>/dev/null || true
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

echo "==> 构建"
cd "$REPO_DIR"
go generate ./...   # 页面资源是 embed 产物，先同步再编译
go build -o "$WORK_DIR/vtuber-agent-go" ./cmd/vtuber-agent-go/
cp -r characters "$WORK_DIR/"
cp scripts/e2e/fakes.py "$WORK_DIR/"

# 前端只在模型目录存在时启用：缺模型时 agent/frontend 会拒绝启动
FRONTEND_BLOCK=""
if [ -d "$MODELS_DIR" ]; then
    FRONTEND_BLOCK=$(cat <<TOML

[frontend]
enabled = true
models_dir = "$MODELS_DIR"
model_dict = "$MODEL_DICT"
model_scale = 0.8
TOML
)
    echo "==> 前端接入已启用（模型目录 $MODELS_DIR）"
else
    echo "==> 未找到模型目录，跳过前端接入（只验证到播报队列）"
fi

cat > "$WORK_DIR/config.toml" <<TOML
[server]
addr = "127.0.0.1:$GW_PORT"

[log]
level = "debug"

[llm]
base_url = "http://127.0.0.1:$LLM_PORT/v1"
api_key = "smoke"
model = "smoke-model"

[agent]
character_file = "characters/mili.toml"
memory_file = "data/memory.jsonl"
enable_tools = true
# 冒烟要覆盖主动发言，把静默阈值压到 2 秒
idle_speak_interval = "2s"

[tts]
engines = ["openai_tts"]

[tts.openai_tts]
base_url = "http://127.0.0.1:$TTS_PORT/v1"
api_key = "smoke"
model = "tts-1"
voice = "alloy"
$FRONTEND_BLOCK

[[clients]]
platform = "qq"
adapter_key = "onebot_v11"
path = "/"

[[clients]]
platform = "bilibili"
adapter_key = "bilibili_live"
path = "/bilibili"
TOML

echo "==> 启动假外部服务"
SMOKE_WORK="$WORK_DIR" SMOKE_LLM_PORT="$LLM_PORT" SMOKE_TTS_PORT="$TTS_PORT" \
    setsid python3 "$WORK_DIR/fakes.py" > "$WORK_DIR/fakes.log" 2>&1 < /dev/null &
sleep 1

echo "==> 启动网关"
cd "$WORK_DIR"
setsid "$WORK_DIR/vtuber-agent-go" > "$WORK_DIR/gateway.log" 2>&1 < /dev/null &

echo "==> 跑客户端断言"
cd "$REPO_DIR"
SMOKE_ADDR="127.0.0.1:$GW_PORT" go run ./scripts/e2e/client

echo "==> 接口检查（/api/*）"
API="http://127.0.0.1:$GW_PORT"

api_get() {
    curl -s --max-time 5 "$API$1"
}

# 配置只读：结构与 config.toml 同构，密钥只回「是否已配置」
config_body="$(api_get /api/config)"
echo "$config_body" | grep -q '"api_key":{"configured":true}' || {
    echo "/api/config 没有回脱敏后的密钥状态: $config_body" >&2
    exit 1
}
if echo "$config_body" | grep -q '"smoke"'; then
    echo "/api/config 泄漏了明文密钥" >&2
    exit 1
fi

# 运行状态：接入端在线、播报计数、上报管线快照
status_body="$(api_get /api/status)"
echo "$status_body" | grep -q '"platforms"' || { echo "/api/status 缺少接入平台: $status_body" >&2; exit 1; }
echo "$status_body" | grep -q '"broadcast"' || { echo "/api/status 缺少播报计数: $status_body" >&2; exit 1; }
echo "$status_body" | grep -q '"queue_size"' || { echo "/api/status 缺少上报管线快照: $status_body" >&2; exit 1; }

# 会话列表与历史
sessions_body="$(api_get /api/sessions)"
echo "$sessions_body" | grep -q 'room_123456' || { echo "/api/sessions 没有列出渠道: $sessions_body" >&2; exit 1; }

history_body="$(api_get /api/sessions/room_123456/history)"
echo "$history_body" | grep -q '"messages"' || { echo "/api/sessions/{id}/history 响应异常: $history_body" >&2; exit 1; }

# 播报注入（新路径）
speak_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST "$API/api/speak" \
    -H 'Content-Type: application/json' -d '{"text":"接口注入冒烟","emotion":"joy"}')"
if [ "$speak_code" != "200" ]; then
    echo "/api/speak 状态码 = $speak_code, want 200" >&2
    exit 1
fi

# 以观众身份发消息：走与平台事件同一条管线，会进会话、生成回复、落记忆
send_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST "$API/api/sessions/room_123456/messages" \
    -H 'Content-Type: application/json' -d '{"user_name":"接口冒烟","text":"接口模拟的观众消息"}')"
if [ "$send_code" != "202" ]; then
    echo "/api/sessions/{id}/messages 状态码 = $send_code, want 202" >&2
    exit 1
fi

for _ in $(seq 1 50); do
    history_body="$(api_get /api/sessions/room_123456/history)"
    echo "$history_body" | grep -q '接口模拟的观众消息' && break
    sleep 0.2
done
if ! echo "$history_body" | grep -q '接口模拟的观众消息'; then
    echo "模拟观众消息没有进会话: $history_body" >&2
    exit 1
fi

# 没配过的平台不该被接受（否则会凭空造出幽灵会话）
bad_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST "$API/api/sessions/weibo_1/messages" \
    -H 'Content-Type: application/json' -d '{"text":"你好"}')"
if [ "$bad_code" != "400" ]; then
    echo "未知渠道状态码 = $bad_code, want 400" >&2
    exit 1
fi

# 路径已切换：旧路径必须下线（注入改走 /api/speak，页面改占根路径）
legacy_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST "$API/inject" \
    -H 'Content-Type: application/json' -d '{"text":"旧路径应已下线"}')"
if [ "$legacy_code" = "200" ]; then
    echo "旧路径 /inject 仍在响应，路径切换没做完" >&2
    exit 1
fi
page_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$API/")"
if [ "$page_code" != "200" ]; then
    echo "首页状态码 = $page_code, want 200（页面没有占住根路径）" >&2
    exit 1
fi

# 浏览器检查要连正在跑的网关，所以放在这里；环境里没有 playwright 会自动跳过
if [ -d "$MODELS_DIR" ]; then
    echo "==> 浏览器检查"
    SMOKE_ADDR="127.0.0.1:$GW_PORT" SMOKE_SHOT="${SMOKE_SHOT:-}" go run ./scripts/e2e/browser
fi

echo "==> 校验落盘结果"
if ! grep -q "已加载角色" "$WORK_DIR/gateway.log"; then
    echo "网关没有加载角色资产" >&2
    exit 1
fi
# 记忆是在本轮对话结束后才落盘的，客户端退出时可能还在写，这里等它一会儿
for _ in $(seq 1 50); do
    [ -s "$WORK_DIR/data/memory.jsonl" ] && break
    sleep 0.1
done
if [ ! -s "$WORK_DIR/data/memory.jsonl" ]; then
    echo "长期记忆没有写入" >&2
    exit 1
fi
if ! grep -q "response_format" "$WORK_DIR/tts.log"; then
    echo "TTS 没有被调用" >&2
    exit 1
fi
if ! grep -q "工具 memory_search 执行完成" "$WORK_DIR/gateway.log"; then
    echo "工具调用没有执行" >&2
    exit 1
fi
if ! grep -q "播报注入已入队" "$WORK_DIR/gateway.log"; then
    echo "播报注入没有生效" >&2
    exit 1
fi
if ! grep -q "接口注入冒烟" "$WORK_DIR/gateway.log"; then
    echo "/api/speak 没有入队" >&2
    exit 1
    echo "播报注入没有生效" >&2
    exit 1
fi
if ! grep -q "主动发言已入队" "$WORK_DIR/gateway.log"; then
    echo "主动发言没有触发" >&2
    exit 1
fi

echo "==> 优雅退出"
GW_PID="$(pgrep -f "^$WORK_DIR/vtuber-agent-go" | head -1)"
if [ -z "$GW_PID" ]; then
    echo "找不到网关进程" >&2
    exit 1
fi
kill -TERM "$GW_PID"
for _ in $(seq 1 50); do
    kill -0 "$GW_PID" 2>/dev/null || break
    sleep 0.1
done
if kill -0 "$GW_PID" 2>/dev/null; then
    echo "收到 SIGTERM 后 5 秒仍未退出" >&2
    kill -9 "$GW_PID" 2>/dev/null || true
    exit 1
fi
if ! grep -q "vtuber-agent-go 已退出" "$WORK_DIR/gateway.log"; then
    echo "退出日志缺失（main 没有走到优雅退出分支）" >&2
    exit 1
fi
# 端口要真的释放掉，不能只是进程没了
if curl -s -o /dev/null --max-time 1 "http://127.0.0.1:$GW_PORT/api/status" 2>/dev/null; then
    echo "网关退出后端口 $GW_PORT 仍在响应" >&2
    exit 1
fi
echo "网关已优雅退出，端口已释放"

echo "==> 关键日志"
grep -E "已加载角色|语音播报已启用|已为渠道|开始播报|播报注入|工具 memory_search|主动发言已入队" "$WORK_DIR/gateway.log" |
    sed 's/.*"msg":"//; s/"}$//' || true

echo
echo "冒烟通过 ✅"
