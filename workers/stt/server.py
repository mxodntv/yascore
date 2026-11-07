#!/usr/bin/env python3
"""faster-whisper HTTP worker. POST /v1/transcribe with a WAV body."""

from __future__ import annotations

import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def make_server(host: str, port: int, transcribe):
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
            if self.path != "/v1/transcribe":
                self.send_error(404)
                return
            n = int(self.headers.get("Content-Length") or 0)
            wav = self.rfile.read(n)
            if len(wav) < 44:
                self._json(400, {"error": "empty audio"})
                return
            try:
                text = transcribe(wav)
            except Exception as exc:  # noqa: BLE001
                self._json(500, {"error": str(exc)})
                return
            self._json(200, {"text": (text or "").strip()})

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


def load_transcriber(model: str, language: str, device: str, prompt: str, hotwords: str):
    from faster_whisper import WhisperModel

    # int8 weights leave VRAM for Ollama; the math stays float16.
    compute = "int8_float16" if device == "cuda" else "int8"
    whisper = WhisperModel(model, device=device, compute_type=compute)

    def transcribe(wav: bytes) -> str:
        # Our own VAD already cut the phrase. Whisper's VAD was eating the first syllable.
        audio = prepare_audio(wav_to_float(wav))
        kwargs = dict(
            language=language or None,
            beam_size=5,
            vad_filter=False,
            condition_on_previous_text=False,
            initial_prompt=prompt or None,
        )
        if hotwords:
            kwargs["hotwords"] = hotwords
        segments, _ = whisper.transcribe(audio, **kwargs)
        return " ".join(seg.text.strip() for seg in segments)

    return transcribe


def wav_to_float(wav: bytes):
    import io
    import wave

    import numpy as np

    with wave.open(io.BytesIO(wav), "rb") as wf:
        rate = wf.getframerate()
        channels = wf.getnchannels()
        width = wf.getsampwidth()
        frames = wf.readframes(wf.getnframes())
    if width != 2:
        raise RuntimeError(f"unsupported wav width {width}")
    audio = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
    if channels > 1:
        audio = audio.reshape(-1, channels).mean(axis=1)
    if rate != 16000 and len(audio) > 1:
        n = max(1, int(len(audio) * 16000 / rate))
        src = np.linspace(0.0, 1.0, num=len(audio), endpoint=False)
        dst = np.linspace(0.0, 1.0, num=n, endpoint=False)
        audio = np.interp(dst, src, audio).astype(np.float32)
    return audio


def prepare_audio(audio):
    import numpy as np

    if len(audio) == 0:
        return audio
    peak = float(np.max(np.abs(audio)))
    # The Fifine sits far below full scale, and Whisper mishears quiet speech.
    if 1e-4 < peak < 0.9:
        audio = audio * (0.9 / peak)
    silence = np.zeros(int(16000 * 0.2), dtype=np.float32)
    return np.concatenate([silence, np.clip(audio, -1.0, 1.0), silence]).astype(np.float32)


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--listen", default="127.0.0.1:8090")
    p.add_argument("--model", default="medium")
    p.add_argument("--language", default="ru")
    p.add_argument("--device", default="cuda")
    p.add_argument("--prompt", default="")
    p.add_argument("--hotwords", default="")
    args = p.parse_args()
    host, port_s = args.listen.rsplit(":", 1)
    server = make_server(
        host,
        int(port_s),
        load_transcriber(args.model, args.language, args.device, args.prompt, args.hotwords),
    )
    print(f"stt listening on {args.listen} model={args.model} device={args.device}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
