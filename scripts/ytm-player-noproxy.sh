#!/bin/sh
# The station player inherits HTTP_PROXY and then asks the PC proxy for
# 127.0.0.1:18883, which is the local audio server. Drop the proxy only
# for ytm-alice-player so YouTube fetches still go through the PC.
# The daemon itself keeps the PC proxy via /data/yam/etc/ytm-proxy.env.
#
# HOME=/tmp/local-ear/player holds an ALSA default that accepts the player's
# AES arguments and plays through Master softvol into dmix. Loopback stays off.
set -eu
script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
if [ -f "$script_dir/local.env" ]; then
  set -a
  . "$script_dir/local.env"
  set +a
fi
# shellcheck disable=SC1091
. "$script_dir/load-config.sh"
: "${CFG_BIN:?set skills.youtube_music.bin}"
: "${CFG_PLAYER_HOME:?set skills.youtube_music.player_home}"
: "${CFG_PROXY_ENV:?set skills.youtube_music.proxy_env}"
: "${CFG_NO_PROXY:?set skills.youtube_music.no_proxy}"
HOST="${CFG_USER}@${CFG_HOST}"
PORT="${CFG_PORT}"
PROXY=$(python3 -c 'from urllib.parse import urlparse; import sys; print(urlparse(sys.argv[1]).hostname)' "$CFG_PUBLIC_BASE")
ssh -o ConnectTimeout=8 -o StrictHostKeyChecking=accept-new -p "$PORT" "$HOST" "PROXY='$PROXY' BIN='$CFG_BIN' PLAYER_HOME='$CFG_PLAYER_HOME' PROXY_ENV='$CFG_PROXY_ENV' NO_PROXY_VALUE='$CFG_NO_PROXY' sh -s" << 'EOS'
set -e
BIN=$(dirname "$(readlink -f "$BIN")")
if [ ! -f "$BIN/ytm-alice-player.bin" ]; then
  mv "$BIN/ytm-alice-player" "$BIN/ytm-alice-player.bin"
fi
mkdir -p "$PLAYER_HOME"
cat > "$PLAYER_HOME/.asoundrc" << 'ASOUND'
pcm.!default {
  @args [ AES0 AES1 AES2 AES3 ]
  @args.AES0 { type integer }
  @args.AES1 { type integer }
  @args.AES2 { type integer }
  @args.AES3 { type integer }
  type plug
  slave.pcm "yascore_vol"
}
pcm.yascore_vol {
  type softvol
  slave.pcm "dmix_spk_plug"
  control { name "Master Playback Volume" card 0 }
}
ASOUND
cat > "$BIN/ytm-alice-player" << EOF
#!/bin/sh
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
export HOME=${PLAYER_HOME}
exec "\$(dirname "\$0")/ytm-alice-player.bin" "\$@"
EOF
chmod 755 "$BIN/ytm-alice-player"
mkdir -p "$(dirname "$PROXY_ENV")"
cat > "$PROXY_ENV" << EOF
export HTTPS_PROXY=http://${PROXY}:8888
export HTTP_PROXY=http://${PROXY}:8888
export https_proxy=http://${PROXY}:8888
export http_proxy=http://${PROXY}:8888
export NO_PROXY=${NO_PROXY_VALUE}
export no_proxy=${NO_PROXY_VALUE}
EOF
INIT=/etc/init.d/S99zytm-alice
if [ -f "$INIT" ] && ! grep -q "$PROXY_ENV" "$INIT"; then
  sed -i "/^export YAM_VAR=/a\\
if [ -f ${PROXY_ENV} ]; then\\
  . ${PROXY_ENV}\\
fi" "$INIT"
fi
echo "player wrapper: $BIN/ytm-alice-player"
echo "proxy env: $PROXY_ENV"
echo "HOME volume: $PLAYER_HOME"
EOS
