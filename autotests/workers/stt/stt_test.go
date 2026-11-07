//go:build integration

package stt_test

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
	rec := stats.Record{Suite: "stt-worker", N: 1, Skip: 1}
	client := http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(cfg.Core.STT.URL + "/healthz")
	if err != nil {
		if pubErr := stats.Publish(rec); pubErr != nil {
			t.Fatal(pubErr)
		}
		t.Skip("stt worker is down")
	}
	resp.Body.Close()
	rec.Skip, rec.Pass = 0, 1
	if pubErr := stats.Publish(rec); pubErr != nil {
		t.Fatal(pubErr)
	}
}
