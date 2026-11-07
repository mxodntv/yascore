#!/bin/sh
# Speak on Mini 2 only with local Silero. Yandex does not synthesize replies.
#
#   scripts/local-voice.sh
#   scripts/local-voice.sh --restore
#
# The column still opens uniproxy so the microphone session and recognition
# exist. Answer text is spoken by Yascore /v1/speak. The bridge does not
# forward TtsGenerate and does not play TtsSpeak.
#
# Apply builds the arm64 bridge from QUASAR_BRIDGE_SRC (default
# /tmp/quasar-bridge), keeps the previous binary once, and restarts the
# bridge. --restore puts that previous binary back. Neither mode reboots
# the column, changes the Yasmina wake bind, or touches the CDN firewall.
# If platformd exits when the bridge restarts, it is started again.
set -eu
script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
if [ -f "$script_dir/local.env" ]; then
  set -a
  . "$script_dir/local.env"
  set +a
fi
# shellcheck disable=SC1091
. "$script_dir/load-config.sh"
HOST="${CFG_USER}@${CFG_HOST}"
PORT="${CFG_PORT}"
SSH="ssh -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new -p $PORT"
SRC="${CFG_BRIDGE_SRC}"
BUILT="${QUASAR_BRIDGE_BIN:-/tmp/quasar-bridge-local-voice}"
MODE=apply
if [ "${1:-}" = "--restore" ]; then
  MODE=restore
elif [ -n "${1:-}" ]; then
  echo "usage: scripts/local-voice.sh [--restore]" >&2
  exit 2
fi

if [ "$MODE" = apply ]; then
  if [ ! -d "$SRC/cmd/quasar-bridge" ]; then
    echo "bridge source not found at $SRC" >&2
    exit 2
  fi
  (cd "$SRC" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$BUILT" ./cmd/quasar-bridge)
fi

$SSH "$HOST" "MODE='$MODE' sh -s" << 'EOS'
set -eu
MODE=$MODE
BIN=$CFG_BRIDGE_BIN
BAK=/data/opt/quasar-bridge/quasar-bridge.before-local-voice
stop_bridge() {
  pid=$(ps | awk '/quasar-bridge/ && !/awk/ {print $1; exit}')
  if [ -z "$pid" ]; then
    rm -f /data/opt/quasar-bridge/bridge.pid
    rmdir /tmp/quasar-bridge-service.lock 2>/dev/null || true
    return 0
  fi
  kill "$pid" 2>/dev/null || true
  i=0
  while [ "$i" -lt 15 ]; do
    if ! ps | awk '/quasar-bridge/ && !/awk/ {found=1} END{exit !found}'; then
      break
    fi
    i=$((i + 1))
    sleep 1
  done
  if ps | awk '/quasar-bridge/ && !/awk/ {found=1} END{exit !found}'; then
    pid=$(ps | awk '/quasar-bridge/ && !/awk/ {print $1; exit}')
    kill -9 "$pid" 2>/dev/null || true
    sleep 1
  fi
  rm -f /data/opt/quasar-bridge/bridge.pid
  rmdir /tmp/quasar-bridge-service.lock 2>/dev/null || true
}
if [ "$MODE" = restore ]; then
  if [ ! -s "$BAK" ]; then
    echo "no backup at $BAK" >&2
    exit 1
  fi
  stop_bridge
  cp -a "$BAK" "$BIN"
  chmod 755 "$BIN"
else
  if [ ! -s "$BAK" ]; then
    cp -a "$BIN" "$BAK"
  fi
  stop_bridge
fi
EOS

if [ "$MODE" = apply ]; then
  $SSH "$HOST" "cat > /data/opt/quasar-bridge/app/bin/quasar-bridge" < "$BUILT"
fi

$SSH "$HOST" "MODE='$MODE' sh -s" << 'EOS'
set -eu
MODE=$MODE
BIN=$CFG_BRIDGE_BIN
chmod 755 "$BIN"
sh /data/opt/quasar-bridge/app/control.sh start
if [ "$MODE" = apply ]; then
  grep -a -q cloud_tts_skipped "$BIN"
else
  if grep -a -q cloud_tts_skipped "$BIN"; then
    echo "restored binary still drops cloud TTS" >&2
    exit 1
  fi
fi
platform_hidden() {
  grep -q " /system/vendor/quasar/maind " /proc/mounts && return 0
  grep -q " /system/vendor/quasar/launch.json " /proc/mounts
}
maind_is_sleep() {
  ps | awk '/sleep 86400/ && !/awk/ {found=1} END{exit !found}'
}
start_platformd() {
  if platform_hidden; then
    return 0
  fi
  if ps | awk '/maind --platformd/ && !/awk/ {found=1} END{exit !found}'; then
    return 0
  fi
  if ! dbus-send --system --print-reply --dest=org.freedesktop.DBus /org/freedesktop/DBus org.freedesktop.DBus.ListNames 2>/dev/null | grep -q 'org.bluez'; then
    /usr/libexec/bluetooth/bluetoothd -d >/tmp/bluetoothd-yasmina.log 2>&1 &
    sleep 1
  fi
  cd /bkupgrade/data/quasar
  LD_LIBRARY_PATH=:/system/vendor/quasar/libs \
  MALLOC_ARENA_MAX=2 \
  CA_CERTIFICATES=/system/vendor/quasar/ca-certificates.crt \
  nohup /system/vendor/quasar/maind --platformd >> /tmp/maind-yasmina.log 2>&1 &
  cd /
}
i=0
while [ "$i" -lt 20 ]; do
  if platform_hidden || maind_is_sleep || ps | awk '/maind --platformd/ && !/awk/ {found=1} END{exit !found}'; then
    break
  fi
  if [ "$i" -eq 2 ]; then
    start_platformd
  fi
  i=$((i + 1))
  sleep 1
done
if ! platform_hidden && ! maind_is_sleep && ! ps | awk '/maind --platformd/ && !/awk/ {found=1} END{exit !found}'; then
  echo "platformd is not running" >&2
  exit 1
fi
count=$(ps | awk '/quasar-bridge/ && !/awk/ {c++} END{print c+0}')
if [ "$count" -ne 1 ]; then
  echo "expected one bridge, found $count" >&2
  exit 1
fi
wget -q -O - http://127.0.0.1:8443/healthz | grep -q '"status":"ok"'
echo "mode=$MODE bridge=1 health=ok"
EOS
