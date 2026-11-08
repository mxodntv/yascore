#!/bin/sh
# Stop the Silero TTS worker started by make-tts.sh.
set -eu
pidfile=/tmp/yascore-tts.pid
if [ -f "$pidfile" ]; then
  pid=$(cat "$pidfile" 2>/dev/null || true)
  if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    i=0
    while [ "$i" -lt 20 ] && kill -0 "$pid" 2>/dev/null; do
      i=$((i + 1))
      sleep 0.2
    done
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  fi
  rm -f "$pidfile"
fi
pkill -f 'workers/tts/server.py' 2>/dev/null || true
echo "tts stopped"
