package stats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// SLO is the phase-1 latency ceiling in milliseconds.
var SLO = map[string]int64{
	"stt": 1000,
	"tts": 3000,
	"llm": 15000,
	"e2e": 15000,
}

// Stage is one latency summary.
type Stage struct {
	P50 int64 `json:"p50_ms"`
	P95 int64 `json:"p95_ms"`
	Max int64 `json:"max_ms"`
	N   int   `json:"n"`
}

// ErrCount groups failures by code.
type ErrCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// Record is one integration run appended to stats.jsonl.
type Record struct {
	TS       string           `json:"ts"`
	GitSHA   string           `json:"git_sha"`
	Suite    string           `json:"suite"`
	N        int              `json:"n"`
	Pass     int              `json:"pass"`
	Fail     int              `json:"fail"`
	Skip     int              `json:"skip"`
	Stages   map[string]Stage `json:"stages,omitempty"`
	Errors   []ErrCount       `json:"errors,omitempty"`
	STTMatch *float64         `json:"stt_match,omitempty"`
}

// FromSamples computes nearest-rank percentiles.
func FromSamples(ms []int64) Stage {
	if len(ms) == 0 {
		return Stage{}
	}
	sorted := append([]int64(nil), ms...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return Stage{N: len(sorted), P50: percentile(sorted, 0.50), P95: percentile(sorted, 0.95), Max: sorted[len(sorted)-1]}
}

func percentile(sorted []int64, p float64) int64 {
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// ModuleRoot walks up from the working directory to go.mod.
func ModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// Publish appends the record, prints the summary, and returns an error on SLO or regression.
func Publish(rec Record) error {
	root := ModuleRoot()
	path := filepath.Join(root, "artifacts", "integration", "stats.jsonl")
	return PublishFile(path, root, rec)
}

// PublishFile is Publish with an explicit path, used by unit tests.
func PublishFile(path, gitDir string, rec Record) error {
	if rec.TS == "" {
		rec.TS = time.Now().UTC().Format(time.RFC3339)
	}
	if rec.GitSHA == "" {
		rec.GitSHA = gitSHA(gitDir)
	}
	prev := lastGreen(path, rec.Suite)
	verdict := evaluate(prev, rec)
	if verdict != nil && rec.Fail == 0 {
		rec.Fail = 1
		rec.Pass = 0
	}
	if err := appendRecord(path, rec); err != nil {
		return err
	}
	fmt.Println(Summary(rec, verdict))
	return verdict
}

// Summary is the human line printed after a run.
func Summary(rec Record, verdict error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "integration suite=%s n=%d pass=%d fail=%d skip=%d sha=%s", rec.Suite, rec.N, rec.Pass, rec.Fail, rec.Skip, rec.GitSHA)
	names := make([]string, 0, len(rec.Stages))
	for name := range rec.Stages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := rec.Stages[name]
		fmt.Fprintf(&b, " %s{p50=%d p95=%d max=%d}", name, st.P50, st.P95, st.Max)
	}
	if rec.STTMatch != nil {
		fmt.Fprintf(&b, " stt_match=%.2f", *rec.STTMatch)
	}
	if verdict != nil {
		fmt.Fprintf(&b, " FAIL %v", verdict)
	} else {
		b.WriteString(" ok")
	}
	return b.String()
}

func evaluate(prev *Record, rec Record) error {
	if rec.Fail > 0 {
		return fmt.Errorf("fail=%d", rec.Fail)
	}
	for name, st := range rec.Stages {
		limit, ok := SLO[name]
		if !ok || st.N == 0 {
			continue
		}
		if st.P95 > limit {
			return fmt.Errorf("%s p95 %dms exceeds slo %dms", name, st.P95, limit)
		}
	}
	if prev == nil {
		return nil
	}
	for name, st := range rec.Stages {
		old, ok := prev.Stages[name]
		// Sub-100ms samples are timer noise and are not compared.
		if !ok || old.N == 0 || old.P95 < 100 || st.N == 0 {
			continue
		}
		if st.P95 > old.P95*125/100 {
			return fmt.Errorf("%s p95 %dms regressed from %dms", name, st.P95, old.P95)
		}
	}
	return nil
}

func appendRecord(path string, rec Record) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return json.NewEncoder(f).Encode(rec)
}

func lastGreen(path, suite string) *Record {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_SH)
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	var found *Record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec Record
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if rec.Suite == suite && rec.Fail == 0 {
			copy := rec
			found = &copy
		}
	}
	return found
}

func gitSHA(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
