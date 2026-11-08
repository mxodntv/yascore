#!/bin/sh
# Point the column bridge at this server. Prompt, words, and URLs come from config.yml.
set -eu
script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/load-config.sh"
: "${CFG_BRIDGE_CONFIG:?set skills.musicyoutube.bridge_config}"
: "${CFG_WAKE_FILE:?set skills.musicyoutube.wake_file}"
: "${CFG_BRIDGE_BIN:?set skills.musicyoutube.bridge_bin}"
HOST="${CFG_USER}@${CFG_HOST}"
SSH="ssh -o ConnectTimeout=20 -o LogLevel=ERROR -p ${CFG_PORT}"
json=/tmp/yascore-config.json

$SSH "$HOST" "cat '$CFG_BRIDGE_CONFIG'" > /tmp/mini2-qb-config.json
python3 - "$json" << 'PY'
import json, sys
from urllib.parse import urlparse
yas = json.load(open(sys.argv[1], encoding="utf-8"))
core = yas["core"]
music = yas["skills"]["musicyoutube"]
pub = urlparse(core["server"]["public_base"])
stt = urlparse(core["stt"]["url"])
cfg = json.load(open("/tmp/mini2-qb-config.json", encoding="utf-8"))
base = f"{pub.scheme}://{pub.hostname}:{pub.port}"
cfg["llm_base_url"] = base + "/v1"
cfg["llm_model"] = core["llm"]["model"]
cfg["llm_system_prompt"] = core["bridge_prompt"].strip()
words = [w.strip() for w in core["wake"]["words"] if w.strip()]
prefixes = list(cfg.get("llm_strip_prefixes") or [])
for word in words:
    for extra in (word, word + ","):
        if extra not in prefixes:
            prefixes.append(extra)
cfg["llm_strip_prefixes"] = prefixes
routing = cfg.setdefault("routing", {})
routing["default_route"] = "llm"
rules = list(routing.get("rules") or [])
if not any(r.get("name") == "config_wake" for r in rules):
    rules.insert(0, {
        "name": "config_wake",
        "route": "llm",
        "match": {"contains_any": words},
    })
routing["rules"] = rules
cfg["idle_wake"] = {
    "enabled": False,
    "interval_seconds": 8,
    "words_file": music["wake_file"],
}
with open("/tmp/mini2-qb-config.new", "w", encoding="utf-8") as f:
    json.dump(cfg, f, ensure_ascii=False, indent=2)
    f.write("\n")
wake = "words:\n" + "".join(f"  - {w}\n" for w in words)
open("/tmp/yascore-wake.yml", "w", encoding="utf-8").write(wake)
open("/tmp/yascore-point.env", "w", encoding="utf-8").write(
    "\n".join([
        f"LLM={base}/v1",
        f"MODEL={core['llm']['model']}",
        f"SPEAK={base}/v1/speak",
        f"STT={pub.scheme}://{pub.hostname}:{stt.port}/v1/transcribe",
        f"WAKE={music['wake_file']}",
        f"BRIDGE={music['bridge_config']}",
        f"BIN={music['bridge_bin']}",
    ]) + "\n"
)
print("patched", base)
PY

# shellcheck disable=SC1091
. /tmp/yascore-point.env
$SSH "$HOST" "cat > '$BRIDGE'" < /tmp/mini2-qb-config.new
$SSH "$HOST" "cat > '$WAKE'" < /tmp/yascore-wake.yml
$SSH "$HOST" "LLM='$LLM' MODEL='$MODEL' SPEAK='$SPEAK' STT='$STT' BIN='$BIN' sh -s" << 'EOS'
set -eu
app=$(CDPATH= cd -- "$(dirname "$(dirname "$BIN")")" && pwd)
root=$(CDPATH= cd -- "$app/.." && pwd)
runtime=$root/runtime.env
control=$app/control.sh
set_env() {
  key=$1
  val=$2
  if grep -q "^${key}=" "$runtime"; then
    sed -i "s|^${key}=.*|${key}=\"${val}\"|" "$runtime"
  else
    echo "${key}=\"${val}\"" >> "$runtime"
  fi
}
set_env QUASAR_LLM_BASE_URL "$LLM"
set_env QUASAR_LLM_MODEL "$MODEL"
set_env QUASAR_ENGLISH_TTS_URL "$SPEAK"
set_env QUASAR_WHISPER_URL "$STT"
sh "$control" restart
sleep 2
sh "$control" status
grep QUASAR_LLM_BASE_URL "$runtime"
EOS
echo "column bridge points at ${CFG_PUBLIC_BASE}"
