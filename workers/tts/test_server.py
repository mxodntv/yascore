import json
import threading
import unittest
import urllib.error
import urllib.request

from server import ResidentSilero, make_server


class TTSTests(unittest.TestCase):
    def test_speak(self):
        wav = b"RIFF" + b"\x00" * 40
        server = make_server("127.0.0.1", 0, lambda text, voice, rate: wav)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.shutdown)
        port = server.server_address[1]
        req = urllib.request.Request(
            f"http://127.0.0.1:{port}/v1/speak",
            data=json.dumps({"text": "привет", "voice": "baya"}).encode(),
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(req) as resp:
            body = resp.read()
        self.assertEqual(body, wav)

    def test_empty_text(self):
        server = make_server("127.0.0.1", 0, lambda text, voice, rate: b"")
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.shutdown)
        port = server.server_address[1]
        req = urllib.request.Request(
            f"http://127.0.0.1:{port}/v1/speak",
            data=json.dumps({"text": "  "}).encode(),
            headers={"Content-Type": "application/json"},
        )
        with self.assertRaises(urllib.error.HTTPError) as caught:
            urllib.request.urlopen(req)
        self.assertEqual(caught.exception.code, 400)


class FakeModel:
    def __init__(self):
        self.calls = 0

    def apply_tts(self, text, speaker, sample_rate):
        self.calls += 1
        return [0.0, 0.25, -0.25]


class ResidentTests(unittest.TestCase):
    def test_one_model_speaks_twice(self):
        model = FakeModel()
        voice = ResidentSilero(model)
        first = voice("привет", "baya", 8000)
        second = voice("ещё", "baya", 8000)
        self.assertEqual(model.calls, 2)
        self.assertTrue(first.startswith(b"RIFF"))
        self.assertTrue(second.startswith(b"RIFF"))
        self.assertGreater(len(first), 44)


if __name__ == "__main__":
    unittest.main()
