#!/usr/bin/env bash
# e2e-linux.sh BINARY [SHOTDIR]: drives the built binary end to end on
# Linux against a real Secret Service (a throwaway gnome-keyring in a
# temporary HOME, in its own D-Bus session and virtual display):
# add / ls / get / run / rotate / done / rm on the command line, and an
# MCP agent's request approved in the running app with the keyboard.
# Only dummy values; the environment is cleared, so nothing from the
# caller's environment reaches the app, the CLI or the commands they run.
# Needs xvfb, dbus, gnome-keyring, openbox, xdotool, imagemagick.
set -euo pipefail
bin=$(realpath "$1"); shots=$(realpath -m "${2:-build/e2e}")
if [ "${3:-}" != --inside ]; then
  exec env -i PATH=/usr/local/bin:/usr/bin:/bin LANG=C.UTF-8 \
    xvfb-run -a -s "-screen 0 1440x900x24" dbus-run-session -- bash "$0" "$bin" "$shots" --inside
fi
mkdir -p "$shots"
export HOME=$(mktemp -d)
export XDG_CONFIG_HOME="$HOME/.config" XDG_DATA_HOME="$HOME/.local/share" KEYBAR_DIR="$HOME/kb"
mkdir -p "$XDG_DATA_HOME/keyrings" "$HOME/bin"
eval "$(printf 'dummy-keyring-password' | gnome-keyring-daemon --replace --unlock --components=secrets)"
export GNOME_KEYRING_CONTROL
ln -s "$bin" "$HOME/bin/keys"
export PATH="$HOME/bin:$PATH"
cd "$HOME"

pass=0; fail=0
ok()   { echo "  ✓ $*"; pass=$((pass+1)); }
bad()  { echo "  ✗ $*"; fail=$((fail+1)); }
has()  { grep -qF -- "$2" <<<"$1" && ok "$3" || { bad "$3"; echo "$1" | sed 's/^/      /'; }; }
hasnt(){ grep -qF -- "$2" <<<"$1" && { bad "$3"; echo "$1" | sed 's/^/      /'; } || ok "$3"; }

V1=dummy-e2e-value-0001
V2=dummy-e2e-value-0002

echo "命令行（真实 Secret Service）"
printf '%s' "$V1" | keys add E2E_TOKEN --desc "e2e 测试" --platform railway:api --platform env:~/proj/.env --tag e2e >/dev/null
out=$(keys ls); has "$out" E2E_TOKEN "ls 列出" ; hasnt "$out" "$V1" "ls 没有值"
out=$(keys doctor 2>&1 || true); has "$out" "钥匙串可用" "doctor：钥匙串可用"
out=$(keys get E2E_TOKEN --reveal </dev/null 2>&1 || true); hasnt "$out" "$V1" "非交互 get，没有批准者 → 拒绝"
out=$(timeout 30 keys run -e T=E2E_TOKEN -- sh -c 'echo run:$T' </dev/null 2>&1 || true); hasnt "$out" "$V1" "非交互 run，没有批准者 → 拒绝"
out=$(keys audit -n 5); has "$out" "E2E_TOKEN" "拒绝记在访问记录里"

echo "菜单栏应用 + MCP 批准"
openbox >/dev/null 2>&1 &
sleep 1
"$bin" >"$shots/app.log" 2>&1 &
app=$!
for _ in $(seq 20); do [ -f "$KEYBAR_DIR/ipc.json" ] && break; sleep 0.5; done
[ -f "$KEYBAR_DIR/ipc.json" ] && ok "应用开了批准通道（ipc.json）" || bad "没有 ipc.json"
mode=$(stat -c %a "$KEYBAR_DIR/ipc.json" 2>/dev/null || echo none); [ "$mode" = 600 ] && ok "ipc.json 是 0600" || bad "ipc.json 权限 $mode"
sleep 2
approve() { # key: answer the request in the panel
  sleep 2.5
  w=$(xdotool search --onlyvisible --name KeyBar | head -1 || true); [ -n "$w" ] || { echo "  (没有可见的面板)"; return 0; }
  xdotool windowactivate --sync "$w" >/dev/null 2>&1 || true
  import -window "$w" "$shots/e2e-$2.png"
  sleep 0.3; xdotool key "$1"
}
req='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e-agent","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_keys","arguments":{}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_with_secrets","arguments":{"command":["sh","-c","echo mcp:$E2E_TOKEN; echo len:${#E2E_TOKEN}"],"keys":["E2E_TOKEN"],"reason":"e2e 测试"}}}'
approve Return mcp-request &
out=$( { printf '%s\n' "$req"; sleep 8; } | timeout 30 keys mcp || true)
wait $! || true
has "$out" '\"name\": \"E2E_TOKEN\"' "list_keys 返回密钥名"
hasnt "$out" "$V1" "MCP 输出里没有值"
has "$out" "mcp:[REDACTED:E2E_TOKEN]" "命令拿到了值，输出被替换"
has "$out" "len:${#V1}" "注入的值长度正确"
approve Escape cli-request &
out=$(timeout 30 keys run -e T=E2E_TOKEN -- sh -c 'echo run:$T' </dev/null 2>&1 || true); wait $! || true
hasnt "$out" "run:$V1" "在应用里按 esc → 拒绝"
approve Return cli-allow &
out=$(timeout 30 keys run -e T=E2E_TOKEN --redact -- sh -c 'echo run:$T' </dev/null 2>&1 || true); wait $! || true
has "$out" "run:[REDACTED:E2E_TOKEN]" "在应用里按 Enter → 允许一次，输出脱敏"
out=$(keys audit -n 10); has "$out" "e2e-agent" "访问记录有 MCP 客户端名"; hasnt "$out" "$V1" "访问记录里没有值"

echo "轮换"
out=$(printf '%s' "$V2" | keys rotate E2E_TOKEN --no-sync 2>&1); has "$out" "Railway · api" "rotate 列出要手动更新的平台"
keys done E2E_TOKEN all >/dev/null
out=$(keys show E2E_TOKEN); has "$out" "E2E_TOKEN" "show 正常"; hasnt "$out" "$V2" "show 没有值"
approve Return rotated &
h2=$(printf '%s' "$V2" | sha256sum | cut -c1-64)   # the command line is logged: no value in it
out=$(timeout 30 keys run -e T=E2E_TOKEN -- sh -c "test \"\$(printf %s \"\$T\" | sha256sum | cut -c1-64)\" = $h2 && echo new-value-ok" </dev/null 2>&1 || true); wait $! || true
has "$out" "new-value-ok" "钥匙串里是新值"
grep -rlF -- "$V1" "$KEYBAR_DIR" && bad "数据目录里出现了旧值" || ok "数据目录里没有旧值"
grep -rlF -- "$V2" "$KEYBAR_DIR" && bad "数据目录里出现了新值" || ok "数据目录里没有新值"

echo "删除"
keys rm E2E_TOKEN -y >/dev/null
out=$(keys ls); hasnt "$out" E2E_TOKEN "rm 后不在清单里"
kill "$app" 2>/dev/null || true
echo "通过 $pass，失败 $fail"
[ "$fail" = 0 ]
