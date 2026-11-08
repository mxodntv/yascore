#!/bin/sh
# Start the Music Assistant docker pilot (PC stream → Yandex Station via Glagol).
# UI: http://<this-host>:8095
# After start: Settings → Player providers → add "Yandex Station", sign in,
# wait for Mini 2 discovery, then play a track and measure next latency.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
dir=$root/contrib/music-assistant
data=${MUSIC_ASSISTANT_DATA:-$HOME/docker/music-assistant-server/data}
mkdir -p "$data"

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not running. Start it once, then re-run this script:" >&2
  echo "  sudo systemctl start docker" >&2
  exit 1
fi

cd "$dir"
MUSIC_ASSISTANT_DATA=$data docker compose pull
MUSIC_ASSISTANT_DATA=$data docker compose up -d
echo "Music Assistant UI: http://$(hostname -I | awk '{print $1}'):8095"
echo "Data dir: $data"
docker compose ps
