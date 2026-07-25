#!/usr/bin/env bash
# 停止后台运行的 lmarena2api
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
PID_FILE="$REPO_ROOT/lmarena2api.pid"
stopped=0

if [[ -f "$PID_FILE" ]]; then
  pid="$(cat "$PID_FILE")"
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" && echo "[+] 已停止 PID=$pid" && stopped=1
  fi
  rm -f "$PID_FILE"
fi

if [[ "$stopped" -eq 0 ]]; then
  # 只按进程名精确匹配，避免误杀路径中含 lmarena2api 的其它进程（比如本脚本自己）
  if pkill -x 'lmarena2api' 2>/dev/null || pkill -x 'lmarena2api-linux' 2>/dev/null; then
    echo "[+] 已通过进程名停止"
  else
    echo "[i] 没有正在运行的 lmarena2api 进程"
  fi
fi
