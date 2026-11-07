//go:build integration

package repo_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yascore/autotests/stats"
	"yascore/internal/repo"
)

func TestSTTLive(t *testing.T) {
	cfg := load(t)
	root := stats.ModuleRoot()
	wavPath := filepath.Join(root, "autotests", "testdata", "audio", "phrase.wav")
	txtPath := filepath.Join(root, "autotests", "testdata", "audio", "phrase.txt")
	wav, wavErr := os.ReadFile(wavPath)
	want, txtErr := os.ReadFile(txtPath)
	if wavErr != nil || txtErr != nil || !reachable(cfg.Core.STT.URL+"/healthz") {
		rec := stats.Record{Suite: "stt", N: 1, Skip: 1}
		if err := stats.Publish(rec); err != nil {
			t.Fatal(err)
		}
		t.Skip("stt worker or autotests/testdata/audio/phrase.wav+phrase.txt is missing")
	}
	start := time.Now()
	text, err := repo.NewHTTPSTT(cfg.Core.STT.URL, time.Duration(cfg.Core.STT.TimeoutSec)*time.Second).Transcribe(context.Background(), wav)
	elapsed := time.Since(start).Milliseconds()
	match := 0.0
	if err == nil && strings.Contains(normalize(text), normalize(string(want))) {
		match = 1
	}
	rec := stats.Record{
		Suite: "stt", N: 1, Pass: 1, STTMatch: &match,
		Stages: map[string]stats.Stage{"stt": stats.FromSamples([]int64{elapsed})},
	}
	if err != nil || match == 0 {
		rec.Pass, rec.Fail = 0, 1
		rec.Errors = []stats.ErrCount{{Code: "empty_transcript", Count: 1}}
	}
	if pubErr := stats.Publish(rec); pubErr != nil {
		t.Fatal(pubErr)
	}
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
