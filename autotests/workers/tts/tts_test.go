//go:build integration

package tts_test

import (
	"net/http"
	"testing"
	"time"

	"yascore/autotests/stats"
	"yascore/config"
)

func TestWorker(t *testing.T) {
	cfg, err := config.LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	rec := stats.Record{Suite: "tts-worker", N: 1, Skip: 1}
	client := http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(cfg.Core.TTS.URL + "/healthz")
	if err != nil {
		if pubErr := stats.Publish(rec); pubErr != nil {
			t.Fatal(pubErr)
		}
		t.Skip("tts worker is down")
	}
	resp.Body.Close()
	rec.Skip, rec.Pass = 0, 1
	if pubErr := stats.Publish(rec); pubErr != nil {
		t.Fatal(pubErr)
	}
}
