//go:build integration

package repo_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"yascore/autotests/stats"
	"yascore/config"
	"yascore/internal/domain"
	"yascore/internal/repo"
)

func TestOllamaLive(t *testing.T) {
	cfg := load(t)
	rec := stats.Record{Suite: "llm", N: 1, Skip: 1}
	if !reachable(cfg.Core.LLM.BaseURL) && !reachable(trimV1(cfg.Core.LLM.BaseURL)) {
		if err := stats.Publish(rec); err != nil {
			t.Fatal(err)
		}
		t.Skip("ollama is down")
	}
	start := time.Now()
	client := repo.NewOllama(cfg.Core.LLM.BaseURL, cfg.Core.LLM.Model, 20*time.Second)
	got, err := client.Complete(context.Background(), domain.CompleteRequest{
		System: "Ответь одним словом: да", Model: cfg.Core.LLM.Model, MaxTokens: 16,
		Messages: []domain.Message{{Role: "user", Content: "скажи да"}},
	})
	elapsed := time.Since(start).Milliseconds()
	rec = stats.Record{Suite: "llm", N: 1, Pass: 1, Stages: map[string]stats.Stage{
		"llm": stats.FromSamples([]int64{elapsed}),
	}}
	if err != nil || got.Text == "" {
		rec.Pass, rec.Fail = 0, 1
		rec.Errors = []stats.ErrCount{{Code: "llm_5xx", Count: 1}}
	}
	if err := stats.Publish(rec); err != nil {
		t.Fatal(err)
	}
}

func load(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func reachable(url string) bool {
	client := http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

func trimV1(base string) string {
	if len(base) > 3 && base[len(base)-3:] == "/v1" {
		return base[:len(base)-3] + "/api/tags"
	}
	return base
}
