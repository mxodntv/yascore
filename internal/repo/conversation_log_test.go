package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"yascore/internal/domain"
)

func TestConversationLogAppendsTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.jsonl")
	logw, err := NewConversationLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logw.Close() })
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file was not created: %v", err)
	}

	logw.Record(domain.Conversation{
		User: "привет", Reply: "здравствуй", Model: "qwen", Stack: "plain model reply",
	})
	logw.Record(domain.Conversation{
		User: "ещё", Error: "llm_5xx", Stack: "goroutine 1 [running]:\nutterance.go:10",
	})

	got := readConversations(t, path)
	if len(got) != 2 {
		t.Fatalf("lines %d", len(got))
	}
	if _, err := time.Parse(time.RFC3339, got[0].TS); err != nil {
		t.Fatalf("ts %q: %v", got[0].TS, err)
	}
	if got[0].User != "привет" || got[0].Reply != "здравствуй" || got[0].Model != "qwen" || got[0].Error != "" {
		t.Fatalf("success %+v", got[0])
	}
	if !strings.Contains(got[0].Stack, "plain model reply") {
		t.Fatalf("branch %q", got[0].Stack)
	}
	if got[0].Tools == nil {
		t.Fatal("tools should be an empty list")
	}
	if got[1].Error != "llm_5xx" || strings.TrimSpace(got[1].Stack) == "" {
		t.Fatalf("error turn %+v", got[1])
	}
	if !strings.Contains(string(mustRead(t, path)), `"tools":[]`) {
		t.Fatalf("tools were omitted: %s", mustRead(t, path))
	}
}

func TestConversationLogCreatesParent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "conversations.jsonl")
	logw, err := NewConversationLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logw.Close() })
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file was not created: %v", err)
	}
}

func TestConversationLogDisabledWhenEmpty(t *testing.T) {
	logw, err := NewConversationLog("  ")
	if err != nil {
		t.Fatal(err)
	}
	if logw.file != nil {
		t.Fatal("empty path opened a file")
	}
	logw.Record(domain.Conversation{User: "привет", Stack: "plain model reply"})
	if err := logw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConversationLogTruncatesAndScrubs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.jsonl")
	logw, err := NewConversationLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logw.Close() })
	long := strings.Repeat("я", maxLogRunes+50)
	logw.Record(domain.Conversation{
		User:  "api_key=supersecretvalue " + long,
		Reply: "Authorization: Bearer abcdefghijk",
		Stack: "token sk-abcdef1234567890",
		Tools: []domain.ToolTrace{{Name: "weather", Arguments: `{"api_key":"supersecretvalue"}`, Result: "ok"}},
	})
	got := readConversations(t, path)
	if len(got) != 1 {
		t.Fatalf("lines %d", len(got))
	}
	if utf8.RuneCountInString(got[0].User) != maxLogRunes {
		t.Fatalf("user runes %d", utf8.RuneCountInString(got[0].User))
	}
	if strings.Contains(got[0].User, "supersecretvalue") || !strings.Contains(got[0].User, "api_key=[redacted]") {
		t.Fatalf("user %q", got[0].User)
	}
	if strings.Contains(got[0].Reply, "abcdefghijk") || !strings.Contains(got[0].Reply, "[redacted]") {
		t.Fatalf("reply %q", got[0].Reply)
	}
	if strings.Contains(got[0].Stack, "sk-abcdef1234567890") {
		t.Fatalf("stack %q", got[0].Stack)
	}
	if strings.Contains(got[0].Tools[0].Arguments, "supersecretvalue") {
		t.Fatalf("args %q", got[0].Tools[0].Arguments)
	}
}

func TestConversationLogConcurrentAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.jsonl")
	logw, err := NewConversationLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logw.Close() })
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			logw.Record(domain.Conversation{User: "раз", Reply: "два", Stack: "plain model reply"})
		}()
	}
	wg.Wait()
	got := readConversations(t, path)
	if len(got) != n {
		t.Fatalf("lines %d", len(got))
	}
}

func readConversations(t *testing.T, path string) []domain.Conversation {
	t.Helper()
	body := mustRead(t, path)
	var out []domain.Conversation
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c domain.Conversation
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		out = append(out, c)
	}
	return out
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
