#!/bin/sh
# Make Mini 2 wake on the Yasmina model that is already on the column.
#
#   scripts/use-yasmina.sh
#   scripts/use-yasmina.sh --restore
#
# maind prefers a downloaded Alisa tflite. The zip comes from the Yandex
# CDN (37.9.64.225, 93.158.134.32, 93.158.134.158). While those addresses
# are rejected, maind falls back to the firmware activation directory.
# The script bind-mounts the on-device Yasmina nnet there, moves the
# downloaded Alisa tflite aside, and starts maind.
#
# The firewall rules and the bind last until the column reboots. After a
# reboot, run the script again. --restore removes the rules, unmounts the
# bind, puts the Alisa tflite back, and starts maind.
# It does not train a model and does not remount the read-only system partition.
set -eu
script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
if [ -f "$script_dir/local.env" ]; then
  set -a
  . "$script_dir/local.env"
  set +a
fi
# shellcheck disable=SC1091
. "$script_dir/load-config.sh"
: "${CFG_SPOTTER_DIR:?set skills.musicyoutube.spotter_dir}"
: "${CFG_SPOTTER_BIND:?set skills.musicyoutube.spotter_bind}"
HOST="${CFG_USER}@${CFG_HOST}"
PORT="${CFG_PORT}"
SSH="ssh -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new -p $PORT"
MODE=apply
if [ "${1:-}" = "--restore" ]; then
  MODE=restore
elif [ -n "${1:-}" ]; then
  echo "usage: scripts/use-yasmina.sh [--restore]" >&2
  exit 2
fi

$SSH "$HOST" "MODE='$MODE' SRC='$CFG_SPOTTER_DIR' SYS='$CFG_SPOTTER_BIND' sh -s" << 'EOS'
set -eu
DST=/data/quasar/data/$(basename "$SRC")-activation
HOLD=/tmp/alisa-tflite-hold
LOG=/tmp/maind-yasmina.log

block_cdn() {
  for ip in 37.9.64.225 93.158.134.32 93.158.134.158; do
    iptables -C OUTPUT -d "$ip" -j REJECT 2>/dev/null || iptables -I OUTPUT 1 -d "$ip" -j REJECT
  done
  ip6tables -C OUTPUT -d 2a02:6b8:23::225 -j REJECT 2>/dev/null || ip6tables -I OUTPUT 1 -d 2a02:6b8:23::225 -j REJECT
}
unblock_cdn() {
  for ip in 37.9.64.225 93.158.134.32 93.158.134.158; do
    while iptables -D OUTPUT -d "$ip" -j REJECT 2>/dev/null; do :; done
  done
  while ip6tables -D OUTPUT -d 2a02:6b8:23::225 -j REJECT 2>/dev/null; do :; done
}
platform_hidden() {
  grep -q " /system/vendor/quasar/maind " /proc/mounts && return 0
  grep -q " /system/vendor/quasar/launch.json " /proc/mounts
}
maind_is_sleep() {
  ps | awk '/sleep 86400/ && !/awk/ {found=1} END{exit !found}'
}
start_maind() {
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
  nohup /system/vendor/quasar/maind --platformd > "$LOG" 2>&1 &
  cd /
}
stop_maind() {
  pid=$(ps | awk '/maind --platformd/ && !/awk/ {print $1; exit}')
  if [ -n "$pid" ]; then
    kill -9 "$pid" 2>/dev/null || true
    sleep 1
  fi
}
hide_tflite() {
  mkdir -p "$HOLD"
  for d in /data/quasar/data/spotter_model/*; do
    [ -f "$d/model.tflite" ] || continue
    mv "$d" "$HOLD/$(basename "$d")"
  done
}
restore_tflite() {
  [ -d "$HOLD" ] || return 0
  for d in "$HOLD"/*; do
    [ -d "$d" ] || continue
    mv "$d" "/data/quasar/data/spotter_model/$(basename "$d")"
  done
}
hears_yasmina() {
  pid=$(ps | awk '/maind --platformd/ && !/awk/ {print $1; exit}')
  [ -n "$pid" ] || return 1
  if ls -l "/proc/$pid/fd" 2>/dev/null | grep 'spotter_model/.*/model\.tflite$' >/dev/null; then
    return 1
  fi
  f=$(ls -l "/proc/$pid/fd" 2>/dev/null | sed -n 's/.* -> //p' | grep 'activation/alisa/acoustic_model.nnet' | head -n 1)
  [ -n "$f" ] || return 1
  grep -q yasmina "$(dirname "$f")/words.txt"
}
hears_alisa() {
  pid=$(ps | awk '/maind --platformd/ && !/awk/ {print $1; exit}')
  [ -n "$pid" ] || return 1
  f=$(ls -l "/proc/$pid/fd" 2>/dev/null | sed -n 's/.* -> //p' | grep 'spotter_model/.*/model\.tflite$' | head -n 1)
  [ -n "$f" ] || return 1
  grep -q '^а ' "$(dirname "$f")/words.txt"
}

if [ "$MODE" = restore ]; then
  unblock_cdn
  umount "$SYS" 2>/dev/null || true
  stop_maind
  restore_tflite
  if platform_hidden || maind_is_sleep; then
    echo "mode=restore ear=active"
    exit 0
  fi
  start_maind
  if maind_is_sleep; then
    echo "mode=restore ear=active"
    exit 0
  fi
  i=0
  while [ "$i" -lt 20 ]; do
    if hears_alisa; then
      break
    fi
    i=$((i + 1))
    sleep 1
  done
  pid=$(ps | awk '/maind --platformd/ && !/awk/ {print $1; exit}')
  if ! hears_alisa; then
    echo "maind ${pid:-none} did not open the Alisa model" >&2
    exit 1
  fi
  echo "mode=restore pid=$pid"
  exit 0
fi

if [ ! -f "$SRC/acoustic_model.nnet" ] || [ ! -f "$SRC/words.txt" ]; then
  echo "column has no yasmina model at $SRC" >&2
  exit 2
fi
mkdir -p "$DST"
cp -a "$SRC"/. "$DST"/
sed -i '/am-tune-shifts/d' "$DST/flags.txt"
if ! grep -q yasmina "$DST/words.txt"; then
  echo "copied model is not yasmina" >&2
  exit 2
fi
if ! grep " $SYS " /proc/mounts >/dev/null 2>&1; then
  mount --bind "$DST" "$SYS"
fi
if ! grep -q yasmina "$SYS/words.txt"; then
  echo "bind did not expose the yasmina model" >&2
  umount "$SYS" 2>/dev/null || true
  exit 1
fi

block_cdn
stop_maind
hide_tflite
if platform_hidden; then
  echo "mode=apply ear=active"
  exit 0
fi
start_maind
if maind_is_sleep; then
  echo "mode=apply ear=active"
  exit 0
fi
i=0
while [ "$i" -lt 20 ]; do
  if hears_yasmina; then
    break
  fi
  i=$((i + 1))
  sleep 1
done
if ! hears_yasmina; then
  unblock_cdn
  umount "$SYS" 2>/dev/null || true
  stop_maind
  restore_tflite
  start_maind
  echo "maind did not open the yasmina model; Alisa restored" >&2
  exit 1
fi
pid=$(ps | awk '/maind --platformd/ && !/awk/ {print $1; exit}')
echo "mode=apply pid=$pid"
echo "words=yasmina"
EOS
