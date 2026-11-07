#!/bin/sh
# Wait until the server from config.yml answers /healthz.
set -eu
json=${1:?config json}
health=$(python3 - "$json" << 'PY'
import json, sys
c = json.load(open(sys.argv[1], encoding="utf-8"))
listen = c["core"]["server"]["listen"]
host, port = listen.rsplit(":", 1)
if host in ("0.0.0.0", ""):
    host = "127.0.0.1"
print(f"http://{host}:{port}/healthz")
PY
)
i=0
while [ "$i" -lt 20 ]; do
  if curl -sf "$health" >/dev/null; then
    echo "yascore ready"
    exit 0
  fi
  i=$((i + 1))
  sleep 0.3
done
echo "yascore failed"
exit 1
