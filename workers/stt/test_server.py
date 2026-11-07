import json
import threading
import unittest
import urllib.error
import urllib.request

from server import make_server


class STTTests(unittest.TestCase):
    def test_transcribe(self):
        server = make_server("127.0.0.1", 0, lambda wav: "привет")
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.shutdown)
        port = server.server_address[1]
        wav = b"RIFF" + b"\x00" * 40
        req = urllib.request.Request(
            f"http://127.0.0.1:{port}/v1/transcribe",
            data=wav,
            headers={"Content-Type": "audio/wav"},
        )
        with urllib.request.urlopen(req) as resp:
            body = json.loads(resp.read().decode())
        self.assertEqual(body["text"], "привет")

    def test_empty_audio(self):
        server = make_server("127.0.0.1", 0, lambda wav: "x")
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.shutdown)
        port = server.server_address[1]
        req = urllib.request.Request(f"http://127.0.0.1:{port}/v1/transcribe", data=b"tiny")
        try:
            urllib.request.urlopen(req)
        except urllib.error.HTTPError as exc:
            self.assertEqual(exc.code, 400)
        else:
            self.fail("expected 400")


if __name__ == "__main__":
    unittest.main()
