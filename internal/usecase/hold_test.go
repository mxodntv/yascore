package usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"yascore/internal/domain"
)

// holdBus is a media skill hub. Pause and resume are recorded in order.
type holdBus struct {
	mu       sync.Mutex
	tools    []domain.Tool
	hints    []string
	events   []string
	pausePos int
	resumes  []int
	llmGate  chan struct{}
}

func (b *holdBus) Tools(context.Context) []domain.Tool { return b.tools }
func (b *holdBus) Hints(context.Context) []string      { return b.hints }
func (b *holdBus) Count() int                          { return len(b.tools) }
func (b *holdBus) Invoke(context.Context, domain.ToolCall) (string, error) {
	return "ok", nil
}

func (b *holdBus) PauseAll(context.Context) error {
	b.mu.Lock()
	b.events = append(b.events, "pause-all")
	b.mu.Unlock()
	return nil
}

func (b *holdBus) PausePlaying(context.Context) (int, error) {
	b.mu.Lock()
	b.events = append(b.events, "pause")
	pos := b.pausePos
	gate := b.llmGate
	b.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		default:
		}
	}
	return pos, nil
}

func (b *holdBus) ResumePlaying(_ context.Context, pos int) error {
	b.mu.Lock()
	b.events = append(b.events, "resume")
	b.resumes = append(b.resumes, pos)
	b.mu.Unlock()
	return nil
}

func (b *holdBus) snapshot() (events []string, resumes []int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.events...), append([]int(nil), b.resumes...)
}

// orderedLLM records whether pause already happened when Complete runs.
type orderedLLM struct {
	bus    *holdBus
	text   string
	paused bool
	seen   int
}

func (l *orderedLLM) Complete(context.Context, domain.CompleteRequest) (domain.Completion, error) {
	l.seen++
	events, _ := l.bus.snapshot()
	for _, ev := range events {
		if ev == "pause" || ev == "pause-all" {
			l.paused = true
			break
		}
	}
	return domain.Completion{Text: l.text, Model: "fake"}, nil
}

func TestHandleUtterancePausesMediaBeforeLLM(t *testing.T) {
	name, words := exampleAssistant(t)
	bus := &holdBus{pausePos: 95025}
	llm := &orderedLLM{bus: bus, text: "Нормально."}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	u.holdQuiet = 30 * time.Millisecond

	reply, err := u.HandleUtterance(context.Background(), name+", как дела")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Нормально." {
		t.Fatalf("reply %q", reply.Text)
	}
	if llm.seen == 0 || !llm.paused {
		t.Fatalf("pause must run before the model: seen=%d paused=%v events=%v", llm.seen, llm.paused, first(bus.snapshot()))
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, resumes := bus.snapshot()
		if len(resumes) == 1 && resumes[0] == 95025 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, resumes := bus.snapshot()
	t.Fatalf("expected resume at 95025, got %v events=%v", resumes, first(bus.snapshot()))
}

func TestWakeHoldSecondCommandCancelsResume(t *testing.T) {
	name, words := exampleAssistant(t)
	bus := &holdBus{pausePos: 12000}
	llm := &fakeLLM{text: "Слушаю."}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	u.holdQuiet = 80 * time.Millisecond

	if _, err := u.HandleUtterance(context.Background(), name+", как дела"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := u.HandleUtterance(context.Background(), name+", ещё раз"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	events, resumes := bus.snapshot()
	pauses := 0
	for _, ev := range events {
		if ev == "pause" {
			pauses++
		}
	}
	if pauses != 2 {
		t.Fatalf("want two wake pauses, events=%v", events)
	}
	// The first quiet window must not resume; the second turn arms a new wait.
	if len(resumes) > 1 {
		t.Fatalf("too many resumes %v from %v", resumes, events)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, resumes = bus.snapshot()
		if len(resumes) == 1 && resumes[0] == 12000 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, resumes = bus.snapshot()
	t.Fatalf("second quiet window should resume once at 12000, got %v", resumes)
}

func TestWakeDoesNotResumeAlreadyStoppedTrack(t *testing.T) {
	name, words := exampleAssistant(t)
	bus := &holdBus{pausePos: -1}
	llm := &fakeLLM{text: "Нормально."}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	u.holdQuiet = 30 * time.Millisecond

	if _, err := u.HandleUtterance(context.Background(), name+", как дела"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	_, resumes := bus.snapshot()
	if len(resumes) != 0 {
		t.Fatalf("stopped track must stay stopped, resumes=%v", resumes)
	}
	if u.mediaHolding() {
		t.Fatal("hold armed for a quiet player")
	}
}

func TestLikeKeepsWakeHoldAndResumes(t *testing.T) {
	name, words := exampleAssistant(t)
	bus := &likeHoldBus{
		holdBus: holdBus{
			pausePos: 42000,
			tools:    []domain.Tool{{Name: "music_control"}},
			hints:    []string{"лайк"},
		},
		body: domain.LikedReply,
	}
	u := New(Deps{LLM: &fakeLLM{text: "не должна"}, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	u.holdQuiet = 30 * time.Millisecond

	reply, err := u.HandleUtterance(context.Background(), name+", лайк")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Quiet || !reply.Instant || reply.Text != domain.LikedReply {
		t.Fatalf("reply %+v", reply)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, resumes := bus.snapshot()
		if len(resumes) == 1 && resumes[0] == 42000 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, resumes := bus.snapshot()
	t.Fatalf("like must resume immediately (Instant), resumes=%v events=%v", resumes, first(bus.snapshot()))
}

func TestQuietForInstantIsZero(t *testing.T) {
	u := New(Deps{})
	u.holdQuiet = time.Second
	if got := u.quietFor(domain.Reply{Text: "Поставила лайк", Instant: true}); got != 0 {
		t.Fatalf("instant quiet %v", got)
	}
	if got := u.quietFor(domain.Reply{Text: "Ок."}); got != time.Second {
		t.Fatalf("normal quiet %v", got)
	}
}

type likeHoldBus struct {
	holdBus
	body string
}

func (b *likeHoldBus) Invoke(context.Context, domain.ToolCall) (string, error) {
	return b.body, nil
}

func TestBareStopWithoutWakeStillStops(t *testing.T) {
	bus := &holdBus{pausePos: 1}
	llm := &fakeLLM{text: "не должна вызваться"}
	_, words := exampleAssistant(t)
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})

	reply, err := u.HandleUtterance(context.Background(), "стоп")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != domain.StoppedReply || !reply.Quiet {
		t.Fatalf("stop reply %+v", reply)
	}
	if len(llm.seen) != 0 {
		t.Fatalf("bare stop called the model: %d", len(llm.seen))
	}
	events, resumes := bus.snapshot()
	if len(resumes) != 0 {
		t.Fatalf("bare stop resumed %v", resumes)
	}
	hasPauseAll := false
	for _, ev := range events {
		if ev == "pause-all" {
			hasPauseAll = true
		}
		if ev == "pause" {
			t.Fatalf("bare stop must not use wake PausePlaying: %v", events)
		}
	}
	if !hasPauseAll {
		t.Fatalf("bare stop must PauseAll, events=%v", events)
	}
}

func first(events []string, _ []int) []string { return events }
