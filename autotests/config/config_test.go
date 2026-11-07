//go:build integration

package config_test

import (
	"testing"
	"time"

	"yascore/autotests/stats"
	"yascore/config"
)

func TestExampleConfig(t *testing.T) {
	start := time.Now()
	cfg, err := config.LoadExample()
	_, validErr := config.Validate(cfg)
	rec := stats.Record{Suite: "config", N: 1, Pass: 1, Stages: map[string]stats.Stage{
		"load": stats.FromSamples([]int64{time.Since(start).Milliseconds()}),
	}}
	if err != nil || validErr != nil {
		rec.Pass, rec.Fail = 0, 1
		rec.Errors = []stats.ErrCount{{Code: "config", Count: 1}}
	}
	if err := stats.Publish(rec); err != nil {
		t.Fatal(err)
	}
}
