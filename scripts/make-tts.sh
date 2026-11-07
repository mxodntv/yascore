#!/bin/sh
# Start the Silero worker from config.yml printed as JSON.
set -eu
json=${1:?config json}
eval "$(python3 - "$json" << 'PY'
import json, shlex, sys
c = json.load(open(sys.argv[1], encoding="utf-8"))
tts = c["core"]["tts"]
missing = [name for name in ("python", "model", "url") if not str(tts.get(name) or "").strip()]
if missing:
    sys.exit("set core.tts." + ", core.tts.".join(missing) + " in config.yml")
url = tts["url"]
listen = url[len("http://"):] if url.startswith("http://") else url
print("PYTHON=" + shlex.quote(tts["python"]))
print("MODEL=" + shlex.quote(tts["model"]))
print("URL=" + shlex.quote(url))
print("LISTEN=" + shlex.quote(listen))
PY
)"
if curl -sf "$URL/healthz" >/dev/null; then
  echo "tts already up"
  exit 0
fi
nohup "$PYTHON" workers/tts/server.py --listen "$LISTEN" --model "$MODEL" >>/tmp/yascore-tts.log 2>&1 &
echo $! >/tmp/yascore-tts.pid
i=0
while [ "$i" -lt 40 ]; do
  if curl -sf "$URL/healthz" >/dev/null; then
    echo "tts ready"
    exit 0
  fi
  i=$((i + 1))
  sleep 1
done
echo "tts failed"
tail -20 /tmp/yascore-tts.log
exit 1
