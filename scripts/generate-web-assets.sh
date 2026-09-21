#!/usr/bin/env bash
#
# 把顶层 frontend/ 的前端源码同步进 internal/backend/web/assets/。
#
# 为什么要有这一步：go:embed 只能嵌入包内文件，而前端源码按决议放在顶层
# frontend/（单一事实源，产物 gitignore）。改完页面必须重跑一次，否则二进制
# 里嵌的还是旧页面。
#
# 用法：go generate ./... 或直接跑本脚本。
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$REPO_DIR/frontend"
DST="$REPO_DIR/internal/backend/web/assets"

[ -f "$SRC/index.html" ] || { echo "前端源码缺失: $SRC/index.html" >&2; exit 1; }

rm -rf "$DST"
mkdir -p "$DST"
cp -R "$SRC/." "$DST/"
touch "$DST/.gitkeep"
echo "已同步前端资源: frontend/ -> internal/backend/web/assets/"
