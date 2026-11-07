#!/bin/sh
# Load config.yml through the Go program and export CFG_* for the column scripts.
# Source this file. It does not parse YAML itself.
set -eu
: "${script_dir:?source this file after setting script_dir}"
root=$(CDPATH= cd -- "$script_dir/.." && pwd)
json=/tmp/yascore-config.json
if [ -x "$root/bin/yascore" ]; then
  "$root/bin/yascore" -config "$root/config.yml" -print > "$json"
else
  (cd "$root" && go run ./cmd/yascore -config config.yml -print > "$json")
fi
eval "$(python3 - "$json" << 'PY'
import json, shlex, sys
c = json.load(open(sys.argv[1], encoding="utf-8"))
m = c["skills"]["youtube_music"]
core = c["core"]
pairs = {
    "CFG_HOST": m.get("host") or "",
    "CFG_PORT": m.get("port") or "",
    "CFG_USER": m.get("user") or "",
    "CFG_BIN": m.get("bin") or "",
    "CFG_SERVICE": m.get("service_url") or "",
    "CFG_NO_PROXY": m.get("no_proxy") or "",
    "CFG_BRIDGE_CONFIG": m.get("bridge_config") or "",
    "CFG_BRIDGE_BIN": m.get("bridge_bin") or "",
    "CFG_BRIDGE_SRC": m.get("bridge_src") or "",
    "CFG_WAKE_FILE": m.get("wake_file") or "",
    "CFG_PROXY_ENV": m.get("proxy_env") or "",
    "CFG_PLAYER_HOME": m.get("player_home") or "",
    "CFG_SPOTTER_DIR": m.get("spotter_dir") or "",
    "CFG_SPOTTER_BIND": m.get("spotter_bind") or "",
    "CFG_PUBLIC_BASE": core["server"].get("public_base") or "",
    "CFG_LLM_MODEL": core["llm"].get("model") or "",
    "CFG_STT_URL": core["stt"].get("url") or "",
    "CFG_TTS_URL": core["tts"].get("url") or "",
    "CFG_TTS_PYTHON": core["tts"].get("python") or "",
    "CFG_TTS_MODEL": core["tts"].get("model") or "",
    "CFG_LISTEN": core["server"].get("listen") or "",
}
for key, value in pairs.items():
    print(f"{key}={shlex.quote(str(value))}")
PY
)"
if [ -z "${CFG_HOST}" ]; then
  echo "skills.youtube_music.host is empty in config.yml" >&2
  exit 1
fi
