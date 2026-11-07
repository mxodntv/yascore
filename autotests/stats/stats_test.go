package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPercentile(t *testing.T) {
	st := FromSamples([]int64{10, 20, 30, 40, 100})
	if st.P50 != 30 || st.Max != 100 || st.N != 5 {
		t.Fatalf("%+v", st)
	}
	if st.P95 != 100 {
		t.Fatalf("p95 %d", st.P95)
	}
}

func TestPublishSLOAndRegression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")
	rec := Record{Suite: "stt", N: 1, Pass: 1, Stages: map[string]Stage{"stt": FromSamples([]int64{200})}}
	if err := PublishFile(path, dir, rec); err != nil {
		t.Fatal(err)
	}
	slow := Record{Suite: "stt", N: 1, Pass: 1, Stages: map[string]Stage{"stt": FromSamples([]int64{1500})}}
	if err := PublishFile(path, dir, slow); err == nil || !strings.Contains(err.Error(), "slo") {
		t.Fatalf("expected slo error, got %v", err)
	}
	ok := Record{Suite: "e2e", N: 1, Pass: 1, Stages: map[string]Stage{"e2e": FromSamples([]int64{1000})}}
	if err := PublishFile(path, dir, ok); err != nil {
		t.Fatal(err)
	}
	regressed := Record{Suite: "e2e", N: 1, Pass: 1, Stages: map[string]Stage{"e2e": FromSamples([]int64{1400})}}
	if err := PublishFile(path, dir, regressed); err == nil || !strings.Contains(err.Error(), "regressed") {
		t.Fatalf("expected regression, got %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "\n") < 4 {
		t.Fatalf("stats not appended:\n%s", raw)
	}
}

func TestPublishFailRecorded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")
	err := PublishFile(path, dir, Record{Suite: "tts", N: 1, Fail: 1, Errors: []ErrCount{{Code: "tts_empty", Count: 1}}})
	if err == nil {
		t.Fatal("expected fail")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "tts_empty") {
		t.Fatalf("missing record %s", raw)
	}
}
