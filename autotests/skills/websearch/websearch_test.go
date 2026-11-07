//go:build integration

package websearch_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yascore/autotests/stats"
	websearch "yascore/skills/websearch"
)

func TestSearchFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a class="result__a" href="http://ex">Курс</a><a class="result__snippet">90 рублей</a>`))
	}))
	defer srv.Close()
	start := time.Now()
	got, err := websearch.Search(t.Context(), srv.URL, "курс доллара")
	elapsed := time.Since(start).Milliseconds()
	rec := stats.Record{Suite: "websearch", N: 1, Pass: 1, Stages: map[string]stats.Stage{
		"search": stats.FromSamples([]int64{elapsed}),
	}}
	if err != nil || got != "Курс. 90 рублей" {
		rec.Pass, rec.Fail = 0, 1
		rec.Errors = []stats.ErrCount{{Code: "search", Count: 1}}
	}
	if pubErr := stats.Publish(rec); pubErr != nil {
		t.Fatal(pubErr)
	}
}
