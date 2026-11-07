package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"yascore/internal/domain"
	"yascore/internal/repo"
)

type memLog struct {
	mu    sync.Mutex
	turns []domain.Conversation
}

func (m *memLog) Record(c domain.Conversation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns = append(m.turns, c)
}

type stackedErr struct {
	msg   string
	stack string
}

func (e stackedErr) Error() string { return e.msg }
func (e stackedErr) Stack() string { return e.stack }

func TestUtteranceLogWritesTwoTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.jsonl")
	logw, err := repo.NewConversationLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logw.Close() })
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file was not created: %v", err)
	}

	name, words := exampleAssistant(t)
	llm := &fakeLLM{text: "здравствуй"}
	u := New(Deps{
		LLM: llm, Wake: fakeWake{words: words}, Log: logw, Model: "qwen", Prompt: "p",
	})
	if _, err := u.HandleUtterance(context.Background(), name+", привет"); err != nil {
		t.Fatal(err)
	}
	llm.err = errors.New("llm_5xx")
	if _, err := u.HandleUtterance(context.Background(), "ещё"); err == nil {
		t.Fatal("expected llm error")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got []domain.Conversation
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c domain.Conversation
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		got = append(got, c)
	}
	if len(got) != 2 {
		t.Fatalf("lines %d: %s", len(got), body)
	}
	if _, err := time.Parse(time.RFC3339, got[0].TS); err != nil {
		t.Fatalf("ts %q: %v", got[0].TS, err)
	}
	if got[0].User != "привет" || got[0].Reply != "здравствуй" || got[0].Error != "" || got[0].Quiet {
		t.Fatalf("success %+v", got[0])
	}
	if !strings.Contains(got[0].Stack, "plain model reply") {
		t.Fatalf("branch %q", got[0].Stack)
	}
	if got[0].Model != "fake" {
		t.Fatalf("model %q", got[0].Model)
	}
	if got[1].User != "ещё" || got[1].Error != "llm_5xx" || strings.TrimSpace(got[1].Stack) == "" {
		t.Fatalf("error %+v", got[1])
	}
	if !strings.Contains(got[1].Stack, "goroutine") {
		t.Fatalf("stack %q", got[1].Stack)
	}
	if got[1].Model != "qwen" {
		t.Fatalf("error model %q", got[1].Model)
	}
}

func TestUtteranceLogBranchesOnce(t *testing.T) {
	name, words := exampleAssistant(t)
	ctx := context.Background()

	t.Run("echo-drop", func(t *testing.T) {
		logw := &memLog{}
		llm := &fakeLLM{text: "Сегодня пятница."}
		u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Log: logw, Prompt: "p"})
		if _, err := u.HandleUtterance(ctx, name+", какой день"); err != nil {
			t.Fatal(err)
		}
		u.HoldSpeech(2 * time.Second)
		_, err := u.HandleUtterance(ctx, name+", Сегодня пятница.")
		if !errors.Is(err, domain.ErrEmpty) {
			t.Fatalf("err %v", err)
		}
		if len(logw.turns) != 2 {
			t.Fatalf("logs %d", len(logw.turns))
		}
		echo := logw.turns[1]
		if echo.User != "Сегодня пятница." || echo.Reply != domain.EchoReply || echo.Error != "" {
			t.Fatalf("echo %+v", echo)
		}
		if !strings.Contains(echo.Stack, "echo-drop") {
			t.Fatalf("stack %q", echo.Stack)
		}
		if len(llm.seen) != 1 {
			t.Fatalf("model calls %d", len(llm.seen))
		}
	})

	t.Run("stop-all", func(t *testing.T) {
		logw := &memLog{}
		u := New(Deps{LLM: &fakeLLM{text: "нет"}, Wake: fakeWake{words: words}, Skills: &memBus{}, Log: logw, Prompt: "p"})
		reply, err := u.HandleUtterance(ctx, name+", стоп")
		if err != nil {
			t.Fatal(err)
		}
		if !reply.Quiet || reply.Text != "Остановилась." {
			t.Fatalf("reply %+v", reply)
		}
		if len(logw.turns) != 1 || !strings.Contains(logw.turns[0].Stack, "stop-all") || !logw.turns[0].Quiet {
			t.Fatalf("log %+v", logw.turns)
		}
		if logw.turns[0].User != "стоп" || logw.turns[0].Reply != "Остановилась." {
			t.Fatalf("log %+v", logw.turns[0])
		}
	})

	t.Run("music-control", func(t *testing.T) {
		logw := &memLog{}
		bus := &memBus{body: domain.SilentReply}
		u := New(Deps{LLM: &fakeLLM{text: "нет"}, Wake: fakeWake{words: words}, Skills: bus, Log: logw, Prompt: "p"})
		if _, err := u.HandleUtterance(ctx, "останови музыку"); err != nil {
			t.Fatal(err)
		}
		if len(logw.turns) != 1 || !strings.Contains(logw.turns[0].Stack, "music-control") {
			t.Fatalf("log %+v", logw.turns)
		}
		if len(logw.turns[0].Tools) != 1 || logw.turns[0].Tools[0].Name != "music_control" {
			t.Fatalf("tools %+v", logw.turns[0].Tools)
		}
		if !strings.Contains(logw.turns[0].Tools[0].Arguments, "stop") {
			t.Fatalf("args %q", logw.turns[0].Tools[0].Arguments)
		}
	})

	t.Run("tool", func(t *testing.T) {
		logw := &memLog{}
		bus := &memBus{
			tools: []domain.Tool{{Name: "weather", Description: "погода"}},
			hints: []string{"погод"},
			body:  "в Москве ясно",
		}
		llm := &fakeLLM{script: []domain.Completion{
			{ToolCalls: []domain.ToolCall{{ID: "1", Name: "weather", Arguments: `{"city":"Москва"}`}}, Model: "fake"},
			{Text: "В Москве ясно.", Model: "fake"},
		}}
		u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Log: logw, Prompt: "p"})
		if _, err := u.HandleUtterance(ctx, name+", какая погода"); err != nil {
			t.Fatal(err)
		}
		if len(logw.turns) != 1 {
			t.Fatalf("logs %d", len(logw.turns))
		}
		turn := logw.turns[0]
		if !strings.Contains(turn.Stack, "weather") || turn.Reply != "В Москве ясно." {
			t.Fatalf("turn %+v", turn)
		}
		if len(turn.Tools) != 1 || turn.Tools[0].Name != "weather" || turn.Tools[0].Result != "в Москве ясно" {
			t.Fatalf("tools %+v", turn.Tools)
		}
		if turn.Tools[0].Arguments != `{"city":"Москва"}` {
			t.Fatalf("args %q", turn.Tools[0].Arguments)
		}
	})
}

func TestUtteranceLogUsesErrorStack(t *testing.T) {
	logw := &memLog{}
	errBoom := stackedErr{msg: "boom", stack: "skill.go:12 boom"}
	u := New(Deps{LLM: &fakeLLM{err: errBoom}, Wake: fakeWake{}, Log: logw, Model: "qwen"})
	_, err := u.HandleUtterance(context.Background(), "привет")
	if !errors.Is(err, errBoom) && (err == nil || err.Error() != "boom") {
		t.Fatalf("err %v", err)
	}
	if len(logw.turns) != 1 {
		t.Fatalf("logs %d", len(logw.turns))
	}
	if logw.turns[0].Error != "boom" || logw.turns[0].Stack != "skill.go:12 boom" {
		t.Fatalf("turn %+v", logw.turns[0])
	}
}

type slowLLM struct {
	*fakeLLM
	delay time.Duration
}

func (s *slowLLM) Complete(ctx context.Context, req domain.CompleteRequest) (domain.Completion, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return s.fakeLLM.Complete(ctx, req)
}

type slowBus struct {
	*memBus
	delay time.Duration
}

func (s *slowBus) Invoke(ctx context.Context, call domain.ToolCall) (string, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return s.memBus.Invoke(ctx, call)
}

type slowTTS struct {
	delay time.Duration
}

func (s *slowTTS) Synthesize(context.Context, string, string) (domain.Audio, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return domain.Audio{MIME: "audio/wav", Bytes: []byte("RIFF"), Voice: "baya"}, nil
}

func TestUtteranceLogCallTimings(t *testing.T) {
	name, words := exampleAssistant(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.jsonl")
	logw, err := repo.NewConversationLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logw.Close() })

	const (
		llmDelay  = 40 * time.Millisecond
		toolDelay = 55 * time.Millisecond
		ttsDelay  = 30 * time.Millisecond
	)
	llm := &slowLLM{
		fakeLLM: &fakeLLM{script: []domain.Completion{
			{ToolCalls: []domain.ToolCall{{ID: "1", Name: "weather", Arguments: `{"city":"Москва"}`}}, Model: "fake"},
			{Text: "В Москве ясно.", Model: "fake"},
		}},
		delay: llmDelay,
	}
	bus := &slowBus{
		memBus: &memBus{
			tools: []domain.Tool{{Name: "weather", Description: "погода"}},
			hints: []string{"погод"},
			body:  "в Москве ясно",
		},
		delay: toolDelay,
	}
	u := New(Deps{
		LLM: llm, Wake: fakeWake{words: words}, Skills: bus,
		TTS: &slowTTS{delay: ttsDelay}, Log: logw, Prompt: "p", Model: "qwen", Voice: "baya",
	})
	if _, _, err := u.Answer(context.Background(), name+", какая погода"); err != nil {
		t.Fatal(err)
	}
	llm.err = errors.New("llm_5xx")
	if _, err := u.HandleUtterance(context.Background(), "ещё"); err == nil {
		t.Fatal("expected llm error")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got []domain.Conversation
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c domain.Conversation
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		got = append(got, c)
	}
	if len(got) != 2 {
		t.Fatalf("lines %d: %s", len(got), body)
	}

	ok := got[0]
	if ok.User != "какая погода" || ok.Reply != "В Москве ясно." || ok.Error != "" {
		t.Fatalf("turn %+v", ok)
	}
	assertTurnClock(t, ok)
	want := []struct {
		name, by string
		min      time.Duration
	}{
		{"llm", "reply", llmDelay},
		{"weather", "reply", toolDelay},
		{"llm", "reply", llmDelay},
		{"tts", "Speak", ttsDelay},
	}
	if len(ok.Calls) != len(want) {
		t.Fatalf("calls %+v", ok.Calls)
	}
	var sum int64
	for i, call := range ok.Calls {
		if call.Name != want[i].name || call.By != want[i].by || call.Error != "" {
			t.Fatalf("call %d %+v", i, call)
		}
		assertCallClock(t, call, want[i].min)
		if call.MS >= ok.MS {
			t.Fatalf("call %d ms %d covers the whole turn %d", i, call.MS, ok.MS)
		}
		sum += call.MS
	}
	if ok.MS < sum {
		t.Fatalf("turn ms %d < calls %d", ok.MS, sum)
	}
	if len(ok.Tools) != 1 || ok.Tools[0].Name != "weather" {
		t.Fatalf("tools %+v", ok.Tools)
	}

	bad := got[1]
	if bad.User != "ещё" || bad.Error != "llm_5xx" || strings.TrimSpace(bad.Stack) == "" {
		t.Fatalf("error turn %+v", bad)
	}
	assertTurnClock(t, bad)
	if len(bad.Calls) != 1 || bad.Calls[0].Name != "llm" || bad.Calls[0].By != "reply" || bad.Calls[0].Error != "llm_5xx" {
		t.Fatalf("error calls %+v", bad.Calls)
	}
	assertCallClock(t, bad.Calls[0], llmDelay)
	if !strings.Contains(string(body), ok.User) || !strings.Contains(string(body), bad.Error) {
		t.Fatalf("file was not appended: %s", body)
	}
}

func assertTurnClock(t *testing.T, c domain.Conversation) {
	t.Helper()
	started, err := time.Parse(time.RFC3339Nano, c.Started)
	if err != nil {
		t.Fatalf("started %q: %v", c.Started, err)
	}
	finished, err := time.Parse(time.RFC3339Nano, c.Finished)
	if err != nil {
		t.Fatalf("finished %q: %v", c.Finished, err)
	}
	if !started.Before(finished) {
		t.Fatalf("started %s finished %s", c.Started, c.Finished)
	}
	if c.MS != finished.Sub(started).Milliseconds() {
		t.Fatalf("ms %d elapsed %d", c.MS, finished.Sub(started).Milliseconds())
	}
}

func assertCallClock(t *testing.T, call domain.CallSpan, min time.Duration) {
	t.Helper()
	start, err := time.Parse(time.RFC3339Nano, call.Start)
	if err != nil {
		t.Fatalf("start %q: %v", call.Start, err)
	}
	end, err := time.Parse(time.RFC3339Nano, call.End)
	if err != nil {
		t.Fatalf("end %q: %v", call.End, err)
	}
	if !start.Before(end) {
		t.Fatalf("call clock %s .. %s", call.Start, call.End)
	}
	elapsed := end.Sub(start).Milliseconds()
	if call.MS != elapsed {
		t.Fatalf("ms %d elapsed %d (%+v)", call.MS, elapsed, call)
	}
	if call.MS < min.Milliseconds()-15 {
		t.Fatalf("ms %d shorter than %s", call.MS, min)
	}
}

func TestHandleChatLogsOnce(t *testing.T) {
	name, words := exampleAssistant(t)
	logw := &memLog{}
	u := New(Deps{LLM: &fakeLLM{text: "Дафт Панк"}, Wake: fakeWake{words: words}, Log: logw, Prompt: "p"})
	_, err := u.HandleChat(context.Background(), []domain.Message{
		{Role: "user", Content: name + ", включи Get Lucky"},
		{Role: "assistant", Content: "ок"},
	}, "кто это?")
	if err != nil {
		t.Fatal(err)
	}
	if len(logw.turns) != 1 || logw.turns[0].User != "кто это?" || logw.turns[0].Reply != "Дафт Панк" {
		t.Fatalf("log %+v", logw.turns)
	}
	if !strings.Contains(logw.turns[0].Stack, "plain model reply") {
		t.Fatalf("stack %q", logw.turns[0].Stack)
	}
}
