#!/usr/bin/env bash
# screenshot-linux.sh BINARY OUTDIR: runs the app on the example data
# (KEYBAR_DEMO=1: an in-memory store, never the real keychain) in a
# virtual X display with a window manager, and saves screenshots of the
# list, a detail and an agent's approval request. Needs xvfb, openbox,
# xdotool, imagemagick and dbus-x11.
set -euo pipefail
bin=$(realpath "$1"); out=$(realpath -m "$2"); mkdir -p "$out"
if [ "${3:-}" != --inside ]; then
  exec xvfb-run -a -s "-screen 0 1440x900x24" dbus-run-session -- bash "$0" "$bin" "$out" --inside
fi
export HOME=$(mktemp -d) KEYBAR_DEMO=1
export XDG_CONFIG_HOME="$HOME/.config"
openbox >/dev/null 2>&1 &
sleep 1; xsetroot -solid '#2f4f6f' || true
shoot() { # name: the panel's window, else the screen
  local w; w=$(xdotool search --onlyvisible --name KeyBar | head -1 || true)
  if [ -n "$w" ]; then import -window "$w" "$out/$1.png"; else import -window root "$out/$1.png"; fi
}
"$bin" >"$out/app.log" 2>&1 &
pid=$!
sleep 8
w=$(xdotool search --onlyvisible --name KeyBar | head -1 || true)
import -window root "$out/linux-desktop.png"
if [ -n "$w" ]; then
  xdotool windowactivate --sync "$w" || true
  shoot linux-list
  sleep 0.5; xdotool type --delay 80 railway; sleep 1; xdotool key Return; sleep 2
  shoot linux-detail
else
  echo "no visible KeyBar window" >>"$out/app.log"
fi
kill "$pid" 2>/dev/null || true
wait "$pid" 2>/dev/null || true
KEYBAR_DEMO_REQUEST=1 "$bin" >>"$out/app.log" 2>&1 &
pid=$!
sleep 8
shoot linux-approval
kill "$pid" 2>/dev/null || true
ls -l "$out"
