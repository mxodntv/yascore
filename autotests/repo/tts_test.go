//go:build integration

package repo_test

import (
	"context"
	"testing"
	"time"

	"yascore/autotests/stats"
	"yascore/internal/repo"
)

func TestTTSLive(t *testing.T) {
	cfg := load(t)
	if !reachable(cfg.Core.TTS.URL + "/healthz") {
		rec := stats.Record{Suite: "tts", N: 1, Skip: 1}
		if err := stats.Publish(rec); err != nil {
			t.Fatal(err)
		}
		t.Skip("tts worker is down")
	}
	start := time.Now()
	audio, err := repo.NewHTTPTTS(cfg.Core.TTS.URL, cfg.Core.TTS.Voice, cfg.Core.TTS.SampleRate, 45*time.Second).
		Synthesize(context.Background(), "Привет, я "+cfg.Core.Name, cfg.Core.TTS.Voice)
	elapsed := time.Since(start).Milliseconds()
	rec := stats.Record{Suite: "tts", N: 1, Pass: 1, Stages: map[string]stats.Stage{
		"tts": stats.FromSamples([]int64{elapsed}),
	}}
	if err != nil || len(audio.Bytes) < 32 {
		rec.Pass, rec.Fail = 0, 1
		rec.Errors = []stats.ErrCount{{Code: "tts_empty", Count: 1}}
	}
	if err := stats.Publish(rec); err != nil {
		t.Fatal(err)
	}
}
