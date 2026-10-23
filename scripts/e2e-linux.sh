#!/usr/bin/env bash
# e2e-linux.sh BINARY [SHOTDIR]: drives the built binary end to end on
# Linux against a real Secret Service (a throwaway gnome-keyring in a
# temporary HOME, in its own D-Bus session and virtual display):
# add / ls / get / run / rotate / done / rm on the command line, import
# with name conflicts, keys run finding a project's keys, connecting
# agents, and an MCP agent's request approved in the running app with the
# keyboard; 自动录入: keys scan --import --clean, keys run asking at the
# terminal for a missing key, an agent's approval with a key pasted in,
# and a key copied to the clipboard saved from the panel.
# Only dummy values; the environment is cleared, so nothing from the
# caller's environment reaches the app, the CLI or the commands they run.
# Needs xvfb, dbus, gnome-keyring, openbox, xdotool, imagemagick, script
# (util-linux); xclip for the clipboard.
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
# clipboard detection defaults off; turn it on so the e2e can exercise it
mkdir -p "$KEYBAR_DIR"
printf '%s\n' '{"remind":true,"remindDays":14,"grantMinutes":15,"cliApproval":true,"clipboard":true}' >"$KEYBAR_DIR/settings.json"

pass=0; fail=0
ok()   { echo "  ✓ $*"; pass=$((pass+1)); }
bad()  { echo "  ✗ $*"; fail=$((fail+1)); }
has()  { grep -qF -- "$2" <<<"$1" && ok "$3" || { bad "$3"; echo "$1" | sed 's/^/      /'; }; }
hasnt(){ grep -qF -- "$2" <<<"$1" && { bad "$3"; echo "$1" | sed 's/^/      /'; } || ok "$3"; }

V1=dummy-e2e-value-0001
V2=dummy-e2e-value-0002
V3=dummy-e2e-value-0003
V4=dummy-e2e-value-0004

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

echo "导入（同名冲突）与自动注入"
proj="$HOME/code/e2e-app"; mkdir -p "$proj/.git" "$proj/src"
printf 'E2E_TOKEN=your-token-here\nE2E_NEW=\nPORT=3000\n' >"$proj/.env.example"
printf 'E2E_TOKEN=%s\nE2E_NEW=dummy-e2e-first-0000\nE2E_NEW=%s\n' "$V3" "$V4" >"$proj/.env"
out=$(cd "$proj" && keys import .env </dev/null 2>&1)
has "$out" "新增 1 个，保留现有 1 个" "导入：同名不同值，非交互默认保留"
has "$out" "出现了 2 次" "导入：同一文件里重复的变量有提示"
hasnt "$out" "$V3" "导入输出里没有值"; hasnt "$out" "$V4" "导入输出里没有值（2）"
out=$(cd "$proj" && keys import .env --on-conflict rename </dev/null 2>&1)
has "$out" "E2E_TOKEN_E2E_APP（E2E_TOKEN 改名导入）" "导入：改名为 NAME_PROJECT"
has "$out" "已有相同值 1 个" "导入：值相同的跳过"
grep -qF 'E2E_TOKEN = "E2E_TOKEN_E2E_APP"' "$proj/keys.toml" && ok "改名记在 keys.toml" || bad "keys.toml 没有映射"
approve Return auto-run &
out=$(cd "$proj/src" && timeout 30 keys run --redact -- sh -c 'echo t:$E2E_TOKEN n:$E2E_NEW p:${PORT:-unset}' </dev/null 2>&1 || true); wait $! || true
has "$out" "t:[REDACTED:E2E_TOKEN_E2E_APP] n:[REDACTED:E2E_NEW] p:unset" "keys run 自动注入（不写配置；改名的按 keys.toml）"
hasnt "$out" "PORT" "keys run：PORT 不提示、不注入（不是密钥名）"
has "$out" "p:unset" "keys run：PORT 未注入"

echo "连接 AI 代理"
unset DSH_HOME CODEX_HOME
mkdir -p "$HOME/.cursor" "$HOME/.codex" "$HOME/.gemini" "$HOME/.config/opencode" "$HOME/.config/goose" "$HOME/.dsh" "$HOME/.config/zed"
printf '{\n  "mcpServers": {\n    "other": {\n      "command": "other-server"\n    }\n  }\n}\n' >"$HOME/.cursor/mcp.json"
printf '# codex\nmodel = "o3"\n' >"$HOME/.codex/config.toml"
printf '{\n  // my theme\n  "theme": "GitHub",\n}\n' >"$HOME/.gemini/settings.json"
printf '{\n  "$schema": "https://opencode.ai/config.json",\n  // providers below\n  "mcp": {\n    "fs": {"type": "local", "command": ["npx", "fs-mcp"]},\n  },\n}\n' >"$HOME/.config/opencode/opencode.jsonc"
printf '# goose\nGOOSE_PROVIDER: openai\nextensions:\n  developer:\n    enabled: true\n    name: developer\n    type: builtin\n' >"$HOME/.config/goose/config.yaml"
printf '[]\n' >"$HOME/.dsh/cordis.patch.yml"
printf '// Zed settings\n{\n  "vim_mode": true,\n}\n' >"$HOME/.config/zed/settings.json"
out=$(keys connect </dev/null 2>&1 || true)
has "$out" "● Cursor" "connect：Cursor 已连接"; has "$out" "● Codex" "connect：Codex 已连接"; has "$out" "未安装：Claude Code" "connect：没装的跳过"
for n in "Gemini CLI" opencode Goose "DeepSeek Harness" Zed; do has "$out" "● $n" "connect：$n 已连接"; done
grep -qF '// my theme' "$HOME/.gemini/settings.json" && grep -qF "\"$bin\"" "$HOME/.gemini/settings.json" && ok "Gemini CLI：注释保留，已合并" || { bad "Gemini 配置"; cat "$HOME/.gemini/settings.json"; }
grep -qF '// providers below' "$HOME/.config/opencode/opencode.jsonc" && grep -qF '"type": "local"' "$HOME/.config/opencode/opencode.jsonc" && grep -qF '"fs"' "$HOME/.config/opencode/opencode.jsonc" && ok "opencode：JSONC 注释保留，command 数组" || { bad "opencode 配置"; cat "$HOME/.config/opencode/opencode.jsonc"; }
grep -qF '"vim_mode": true' "$HOME/.config/zed/settings.json" && grep -qF '"context_servers"' "$HOME/.config/zed/settings.json" && ok "Zed：context_servers" || { bad "Zed 配置"; cat "$HOME/.config/zed/settings.json"; }
grep -qF '  developer:' "$HOME/.config/goose/config.yaml" && grep -qF "cmd: \"$bin\"" "$HOME/.config/goose/config.yaml" && grep -qF 'args: ["mcp", "--client", "goose"]' "$HOME/.config/goose/config.yaml" && ok "Goose：extensions.keybar，developer 保留" || { bad "Goose 配置"; cat "$HOME/.config/goose/config.yaml"; }
grep -qF "name: '@deepseek-ai/dsh-mcp-client'" "$HOME/.dsh/cordis.patch.yml" && grep -qF 'serverName: keybar' "$HOME/.dsh/cordis.patch.yml" && ! grep -qx '\[\]' "$HOME/.dsh/cordis.patch.yml" && ok "DeepSeek Harness：cordis.patch.yml 一条 insert" || { bad "dsh 配置"; cat "$HOME/.dsh/cordis.patch.yml"; }
out=$(keys connect --status 2>&1); for n in Goose "DeepSeek Harness" opencode; do has "$out" "● $n" "status 读回 $n 已连接"; done
out=$(keys disconnect goose deepseek-harness 2>&1)
grep -qF keybar "$HOME/.config/goose/config.yaml" && bad "Goose 没断开" || ok "Goose 断开"
[ "$(cat "$HOME/.dsh/cordis.patch.yml")" = "[]" ] && ok "dsh 断开，恢复 []" || { bad "dsh 断开"; cat "$HOME/.dsh/cordis.patch.yml"; }
grep -qF "\"$bin\"" "$HOME/.cursor/mcp.json" && grep -qF '"other-server"' "$HOME/.cursor/mcp.json" && ok "Cursor 配置合并，原有的保留" || { bad "Cursor 配置"; cat "$HOME/.cursor/mcp.json"; }
[ -f "$HOME/.cursor/mcp.json.keybar-backup" ] && ok "原文件已备份" || bad "没有备份"
grep -qF 'args = ["mcp", "--client", "codex"]' "$HOME/.codex/config.toml" && grep -qF 'model = "o3"' "$HOME/.codex/config.toml" && ok "Codex 配置合并" || { bad "Codex 配置"; cat "$HOME/.codex/config.toml"; }
before=$(sha256sum "$HOME/.cursor/mcp.json"); keys connect >/dev/null 2>&1 || true
[ "$before" = "$(sha256sum "$HOME/.cursor/mcp.json")" ] && ok "再次 connect 不改文件" || bad "再次 connect 改了文件"
cfg='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
cmd=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["mcpServers"]["keybar"]["command"])' "$HOME/.cursor/mcp.json" 2>/dev/null || echo "$bin")
out=$( { printf '%s\n' "$cfg"; sleep 1; } | timeout 10 "$cmd" mcp --client cursor || true)
has "$out" '"run_with_secrets"' "按写入的配置启动 MCP 服务"
out=$(keys disconnect cursor 2>&1); grep -qF keybar "$HOME/.cursor/mcp.json" && bad "disconnect 没删掉" || ok "disconnect"

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
grep -rlF -e "$V3" -e "$V4" "$KEYBAR_DIR" && bad "数据目录里出现了导入的值" || ok "数据目录里没有导入的值"

echo "自动录入：扫描本机、运行时补录、剪贴板"
F=E2eQx7Lp2Wm9Vb4Nr8Ty3Hs6Jc1Fg5Zu0   # 33 characters of filler: key-shaped dummies, never real
E_OPENAI="sk-proj-$F$F"; E_GH="ghp_${F}AB"; E_GROQ="gsk_$F$F"; E_ANT="sk-ant-api03-$F$F"; E_HF="hf_$F"; E_XAI="xai-$F$F"
mkdir -p "$HOME/code/shop/node_modules/x" "$HOME/projects/tool/.git"
printf 'PORT=3000\nOPENAI_API_KEY=%s\nDB_PASSWORD=e2e-hunter2-dummy\n' "$E_OPENAI" >"$HOME/code/shop/.env"
printf 'OPENAI_API_KEY=your-key\n' >"$HOME/code/shop/.env.example"
printf 'X_TOKEN=%s\n' "$E_HF" >"$HOME/code/shop/node_modules/x/.env"
printf 'export PATH="$HOME/bin:$PATH"\nexport GITHUB_TOKEN=%s\nexport EDITOR=vim\n' "$E_GH" >"$HOME/.zshrc"
out=$(keys scan 2>&1)
has "$out" "~/code/shop/.env" "scan 找到项目的 .env"
has "$out" "~/.zshrc" "scan 找到 shell 配置里的 export"
has "$out" "sk-proj-… 74 位" "scan 只显示前缀和长度"
has "$out" "· PORT" "PORT 不勾选"
hasnt "$out" "$E_OPENAI" "scan 输出里没有 OpenAI 值"; hasnt "$out" "$E_GH" "没有 GitHub 值"; hasnt "$out" "hunter2" "没有密码"
hasnt "$out" "node_modules" "跳过 node_modules"; hasnt "$out" ".env.example" "跳过 .env.example"
out=$(keys scan --import --clean names 2>&1)
has "$out" "备份在 ~/code/shop/.env.keybar-backup" "导入后移除明文，先备份"
out=$(keys ls); has "$out" OPENAI_API_KEY "导入了 .env 的 key"; has "$out" GITHUB_TOKEN "导入了 .zshrc 的 export"
keys show PORT >/dev/null 2>&1 && bad "PORT 被导入了" || ok "PORT 没有导入"
grep -qxF 'OPENAI_API_KEY=' "$HOME/code/shop/.env" && grep -qxF 'PORT=3000' "$HOME/code/shop/.env" && ok ".env 只留变量名" || { bad ".env 清理"; cat "$HOME/code/shop/.env"; }
grep -qF '# moved to KeyBar: GITHUB_TOKEN' "$HOME/.zshrc" && ! grep -qF "$E_GH" "$HOME/.zshrc" && grep -qF 'export EDITOR=vim' "$HOME/.zshrc" && ok ".zshrc 的 export 改为注释，其余不动" || { bad ".zshrc 清理"; cat "$HOME/.zshrc"; }
grep -qF "$E_OPENAI" "$HOME/code/shop/.env.keybar-backup" && ok "备份是原文件" || bad "备份"
out=$(keys audit -n 30); has "$out" "扫描本机" "扫描记在访问记录里"; has "$out" "移除明文" "移除明文记在访问记录里"
grep -rlF -e "$E_OPENAI" -e "$E_GH" "$KEYBAR_DIR" && bad "数据目录里出现了扫描的值" || ok "数据目录里没有扫描的值"

mkdir -p "$HOME/code/fill/.git"; printf 'GROQ_API_KEY=\nPORT=3000\nNODE_ENV=dev\n' >"$HOME/code/fill/.env.example"
( cd "$HOME/code/fill" && { sleep 2; printf '%s\n' "$E_GROQ"; sleep 2; } | timeout 30 script -qec "keys run -- sh -c 'echo child-got-\${#GROQ_API_KEY}'" /dev/null ) >"$shots/missing-key-tty.raw" 2>&1 || true
tr -d '\r' <"$shots/missing-key-tty.raw" >"$shots/missing-key-tty.txt"; rm -f "$shots/missing-key-tty.raw"
out=$(cat "$shots/missing-key-tty.txt")
has "$out" "GROQ_API_KEY 不在 KeyBar，粘贴值保存，回车跳过" "终端里提示补上缺的 key"
has "$out" "已存入 GROQ_API_KEY（gsk_… 70 位）" "补上的存进钥匙串"
has "$out" "child-got-70" "补上的值注入了子进程"
hasnt "$out" "$E_GROQ" "终端记录里没有值（输入不回显）"
hasnt "$out" "PORT 不在" "PORT、NODE_ENV 不问"

mkdir -p "$HOME/code/agent/.git"; printf 'XAI_API_KEY=\nPORT=1\n' >"$HOME/code/agent/.env.example"
fill() { # paste a value into the approval's field, then Enter
  sleep 2.5
  w=$(xdotool search --onlyvisible --name KeyBar | head -1 || true); [ -n "$w" ] || { echo "  (没有可见的面板)"; return 0; }
  xdotool windowactivate --sync "$w" >/dev/null 2>&1 || true
  sleep 0.3; xdotool type --delay 8 "$1"; sleep 0.5
  import -window "$w" "$shots/e2e-missing-key.png"
  xdotool key Return
}
req='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e-agent","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run_with_secrets","arguments":{"command":["sh","-c","echo xai-len:${#XAI_API_KEY}"],"cwd":"'"$HOME/code/agent"'","reason":"e2e 补录"}}}'
fill "$E_XAI" &
out=$( { printf '%s\n' "$req"; sleep 10; } | timeout 30 keys mcp || true)
wait $! || true
has "$out" "xai-len:70" "代理请求里粘贴补上的 key 一起注入"
hasnt "$out" "$E_XAI" "MCP 输出里没有补上的值"
keys show XAI_API_KEY >/dev/null 2>&1 && ok "补上的存进钥匙串" || bad "XAI_API_KEY 没存上"

if command -v xclip >/dev/null; then
  printf '%s' "$E_ANT" | xclip -selection clipboard
  sleep 4
  xdotool key alt+shift+k; sleep 1.5
  w=$(xdotool search --onlyvisible --name KeyBar | head -1 || true)
  if [ -n "$w" ]; then
    xdotool windowactivate --sync "$w" >/dev/null 2>&1 || true
    import -window "$w" "$shots/e2e-clipboard.png"
    sleep 0.3; xdotool key ctrl+s; sleep 1; xdotool key Escape
  fi
  out=$(keys ls); has "$out" ANTHROPIC_API_KEY "剪贴板里的 key 一键保存"
  out=$(keys audit -n 5); has "$out" "从剪贴板保存" "保存记在访问记录里"
  grep -rlF -- "$E_ANT" "$KEYBAR_DIR" "$shots/app.log" && bad "剪贴板内容写进了文件" || ok "剪贴板内容没有写进任何文件"
  printf 'not a key' | xclip -selection clipboard
else
  echo "  (没有 xclip：跳过剪贴板)"
fi

echo "删除"
keys rm E2E_TOKEN -y >/dev/null; keys rm E2E_TOKEN_E2E_APP -y >/dev/null; keys rm E2E_NEW -y >/dev/null
out=$(keys ls); hasnt "$out" E2E_ "rm 后不在清单里"
kill "$app" 2>/dev/null || true
echo "通过 $pass，失败 $fail"
[ "$fail" = 0 ]
