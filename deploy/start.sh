#!/usr/bin/env bash
# Linux / WSL / macOS 下启动 lmarena2api
#   前台运行： ./deploy/start.sh
#   后台运行： ./deploy/start.sh -d
#   停止：     ./deploy/stop.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
ENV_FILE="${ENV_FILE:-$SCRIPT_DIR/.env}"
BACKGROUND=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--daemon)   BACKGROUND=1; shift ;;
    -e|--env-file) ENV_FILE="$2"; shift 2 ;;
    -b|--bin)      BIN="$2"; shift 2 ;;
    -h|--help)     sed -n '2,6p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

# ---------- 1. 配置文件 ----------
if [[ ! -f "$ENV_FILE" ]]; then
  echo "[!] 未找到配置文件: $ENV_FILE"
  if [[ -f "$SCRIPT_DIR/.env.example" ]]; then
    cp "$SCRIPT_DIR/.env.example" "$ENV_FILE"
    echo "[+] 已生成 $ENV_FILE，请填写 LA_COOKIE / CF_CLEARANCE / USER_AGENT 后重新运行。"
  fi
  exit 1
fi

# 逐行解析 KEY=VALUE，不用 source —— User-Agent 里的空格和括号会让 source 报语法错误
while IFS= read -r raw || [[ -n "$raw" ]]; do
  line="${raw%$'\r'}"                      # 去掉 Windows 换行符
  [[ -z "${line// }" || "${line#"${line%%[![:space:]]*}"}" == \#* ]] && continue
  [[ "$line" != *=* ]] && continue
  key="${line%%=*}"; val="${line#*=}"
  key="$(printf '%s' "$key" | tr -d '[:space:]')"
  val="${val#"${val%%[![:space:]]*}"}"; val="${val%"${val##*[![:space:]]}"}"
  if [[ ${#val} -ge 2 && ( ( "$val" == \"*\" ) || ( "$val" == \'*\' ) ) ]]; then
    val="${val:1:${#val}-2}"
  fi
  [[ -z "$val" ]] && continue
  export "$key=$val"
done < "$ENV_FILE"

: "${LA_COOKIE:?[x] .env 中缺少 LA_COOKIE}"
[[ -z "${CF_CLEARANCE:-}" ]] && echo "[i] 未设置 CF_CLEARANCE（多数账号本就没有此 cookie），请求将不携带它。"
[[ -z "${USER_AGENT:-}" ]] && echo "[!] 未设置 USER_AGENT，将使用内置 Mac UA；Cloudflare 校验失败时请填入浏览器真实 UA。"

# ---------- 2. 可执行文件 ----------
if [[ -z "${BIN:-}" ]]; then
  for c in "$REPO_ROOT/lmarena2api" "$REPO_ROOT/lmarena2api-linux" "$REPO_ROOT/lmarena2api-macos"; do
    [[ -x "$c" ]] && BIN="$c" && break
  done
fi
if [[ -z "${BIN:-}" ]]; then
  if command -v go >/dev/null 2>&1; then
    echo "[i] 未找到二进制，正在用 go build 编译..."
    (cd "$REPO_ROOT" && go build -o lmarena2api)
    BIN="$REPO_ROOT/lmarena2api"
  else
    echo "[x] 找不到可执行文件，也没有安装 Go。请从 Releases 下载对应平台文件放到仓库根目录。"
    exit 1
  fi
fi
chmod +x "$BIN" 2>/dev/null || true

PORT="${PORT:-10088}"
echo "[+] 可执行文件 : $BIN"
echo "[+] 接口地址   : http://127.0.0.1:${PORT}/v1/chat/completions"

# ---------- 3. 启动 ----------
cd "$REPO_ROOT"
if [[ "$BACKGROUND" -eq 1 ]]; then
  nohup "$BIN" > "$REPO_ROOT/logfile.log" 2>&1 &
  echo $! > "$REPO_ROOT/lmarena2api.pid"
  echo "[+] 已后台启动，PID=$(cat "$REPO_ROOT/lmarena2api.pid")，日志: logfile.log"
  echo "    查看日志: tail -f logfile.log"
else
  echo "[+] 前台运行，Ctrl+C 停止"
  exec "$BIN"
fi
