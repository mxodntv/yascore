#!/usr/bin/env python3
"""Silero HTTP worker. POST /v1/speak {"text","voice","sample_rate"} -> audio/wav."""

from __future__ import annotations

import argparse
import array
import io
import json
import threading
import wave
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def make_server(host: str, port: int, runner):
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path != "/healthz":
                self.send_error(404)
                return
            body = b'{"status":"ok"}'
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            if self.path != "/v1/speak":
                self.send_error(404)
                return
            n = int(self.headers.get("Content-Length") or 0)
            try:
                payload = json.loads(self.rfile.read(n).decode() or "{}")
            except json.JSONDecodeError:
                self._json(400, {"error": "bad json"})
                return
            text = str(payload.get("text") or "").strip()
            if not text:
                self._json(400, {"error": "empty text"})
                return
            voice = str(payload.get("voice") or "baya")
            rate = int(payload.get("sample_rate") or 48000)
            try:
                wav = runner(text, voice, rate)
            except Exception as exc:  # noqa: BLE001
                self._json(500, {"error": str(exc)})
                return
            if not wav or len(wav) < 32:
                self._json(500, {"error": "tts_empty"})
                return
            self.send_response(200)
            self.send_header("Content-Type", "audio/wav")
            self.send_header("Content-Length", str(len(wav)))
            self.end_headers()
            self.wfile.write(wav)

        def _json(self, code: int, payload: dict) -> None:
            raw = json.dumps(payload, ensure_ascii=False).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def log_message(self, fmt: str, *args) -> None:
            return

    return ThreadingHTTPServer((host, port), Handler)


def pcm_wav(audio, sample_rate: int) -> bytes:
    if hasattr(audio, "detach"):
        audio = audio.detach()
    if hasattr(audio, "cpu"):
        audio = audio.cpu()
    if hasattr(audio, "numpy"):
        audio = audio.numpy()
    frames = array.array("h")
    for sample in audio:
        value = float(sample)
        if value > 1:
            value = 1.0
        if value < -1:
            value = -1.0
        frames.append(int(value * 32767))
    buf = io.BytesIO()
    with wave.open(buf, "wb") as out:
        out.setnchannels(1)
        out.setsampwidth(2)
        out.setframerate(int(sample_rate))
        out.writeframes(frames.tobytes())
    return buf.getvalue()


class ResidentSilero:
    """One loaded Silero model. Each phrase is synthesized from it."""

    def __init__(self, model):
        self.model = model
        self.lock = threading.Lock()

    def __call__(self, text: str, voice: str, sample_rate: int) -> bytes:
        with self.lock:
            audio = self.model.apply_tts(text=text, speaker=voice, sample_rate=sample_rate)
        return pcm_wav(audio, sample_rate)


def load_silero(model_path: str):
    import torch

    model = torch.package.PackageImporter(model_path).load_pickle("tts_models", "model")
    model.to(torch.device("cpu"))
    return model


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--listen", default="127.0.0.1:8092")
    # Kept so the previous command line still starts. The model is loaded here, not in a new CLI per phrase.
    p.add_argument("--python", default="")
    p.add_argument("--cli", default="")
    p.add_argument("--model", required=True)
    args = p.parse_args()
    host, port_s = args.listen.rsplit(":", 1)
    model = load_silero(args.model)
    print("tts model ready", flush=True)
    server = make_server(host, int(port_s), ResidentSilero(model))
    print(f"tts listening on {args.listen}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
