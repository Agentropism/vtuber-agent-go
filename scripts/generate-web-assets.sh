#!/usr/bin/env bash
#
# 把顶层 frontend/ 的前端源码同步进 internal/backend/web/assets/。
#
# 为什么要有这一步：go:embed 只能嵌入包内文件，而前端源码按决议放在顶层
# frontend/（单一事实源，产物 gitignore）。改完页面必须重跑一次，否则二进制
# 里嵌的还是旧页面。
#
# 调试台（frontend/debug）是 Vite 工程，需要 Node 构建；node_modules 不存在时
# 先 npm ci（npmmirror 源）。WSL 里没有 node 时会用 Windows 的 npm（互操作），
# 两条路都不可用时明确报错而不是嵌一个空目录。
#
# 用法：go generate ./... 或直接跑本脚本。
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$REPO_DIR/frontend"
DST="$REPO_DIR/internal/backend/web/assets"
DEBUG_SRC="$SRC/debug"

[ -f "$SRC/index.html" ] || { echo "前端源码缺失: $SRC/index.html" >&2; exit 1; }

# 调试台：产物 dist/ 不进仓库，所以每次 generate 都重建（增量构建很快）
if [ -f "$DEBUG_SRC/package.json" ]; then
  # Node 优先用用户级安装（~/tools/node，与 Go 同一套路），其次 PATH 里已有的
  if [ -x "$HOME/tools/node/bin/node" ]; then
    export PATH="$HOME/tools/node/bin:$PATH"
  fi
  if ! command -v npm >/dev/null 2>&1; then
    echo "错误: 需要 Node/npm 构建调试台（或先在 Windows 侧 npm run build）" >&2
    exit 1
  fi
  if [ ! -d "$DEBUG_SRC/node_modules" ]; then
    (cd "$DEBUG_SRC" && npm ci --registry=https://registry.npmmirror.com --no-audit --no-fund)
  fi
  (cd "$DEBUG_SRC" && npm run build)
  [ -f "$DEBUG_SRC/dist/index.html" ] || { echo "调试台构建产物缺失: $DEBUG_SRC/dist" >&2; exit 1; }
fi

rm -rf "$DST"
mkdir -p "$DST"

# 舞台页：frontend/ 下除 debug/（调试台源码目录）之外的内容照旧拷
for entry in "$SRC"/*; do
  name="$(basename "$entry")"
  [ "$name" = "debug" ] && continue
  cp -R "$entry" "$DST/"
done

# 调试台构建产物
mkdir -p "$DST/debug"
cp -R "$DEBUG_SRC/dist/." "$DST/debug/"

touch "$DST/.gitkeep"
echo "已同步前端资源: frontend/ -> internal/backend/web/assets/（含 debug/ 调试台产物）"
