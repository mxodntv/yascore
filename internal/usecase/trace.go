package usecase

import (
	"context"
	"strings"
	"sync"
	"time"

	"yascore/internal/domain"
)

// callTrace collects outbound steps for one utterance.
// The owner is the outermost handler; nested handlers share the same trace.
type callTrace struct {
	mu      sync.Mutex
	ctx     context.Context
	started time.Time
	calls   []domain.CallSpan
	rec     domain.Conversation
	has     bool
}

type traceKey struct{}

func beginTrace(ctx context.Context) (context.Context, *callTrace) {
	if ctx == nil {
		ctx = context.Background()
	}
	if traceFrom(ctx) != nil {
		return ctx, nil
	}
	tr := &callTrace{started: time.Now(), ctx: ctx}
	return context.WithValue(ctx, traceKey{}, tr), tr
}

func traceFrom(ctx context.Context) *callTrace {
	if ctx == nil {
		return nil
	}
	tr, _ := ctx.Value(traceKey{}).(*callTrace)
	return tr
}

func (tr *callTrace) stash(c domain.Conversation) {
	tr.mu.Lock()
	tr.rec = c
	tr.has = true
	tr.mu.Unlock()
}

func (tr *callTrace) add(span domain.CallSpan) {
	tr.mu.Lock()
	tr.calls = append(tr.calls, span)
	tr.mu.Unlock()
}

// noteFailure keeps a step error on the turn when the reply was already stashed.
func noteFailure(ctx context.Context, err error) {
	tr := traceFrom(ctx)
	if tr == nil || err == nil {
		return
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.has && tr.rec.Error == "" {
		tr.rec.Error = err.Error()
	}
}

// timed measures one outbound call and appends it to the open trace.
// The same step is copied onto the command record when one is open.
func (u *Utterance) timed(ctx context.Context, name, by, detail string, fn func() error) error {
	start := time.Now()
	err := fn()
	end := time.Now()
	name = strings.TrimSpace(name)
	if name == "" {
		name = "call"
	}
	tr := traceFrom(ctx)
	if tr != nil {
		startS, endS, ms := spanClock(start, end)
		span := domain.CallSpan{Name: name, By: by, Start: startS, End: endS, MS: ms}
		if err != nil {
			span.Error = err.Error()
		}
		tr.add(span)
	}
	NoteAction(ctx, name, by, detail, start, end, err)
	return err
}

func toolDetail(call domain.ToolCall) string {
	name := strings.TrimSpace(call.Name)
	args := strings.TrimSpace(call.Arguments)
	if name == "" {
		return args
	}
	if args == "" {
		return name
	}
	return name + " " + args
}

func toolCallName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "tool"
	}
	return name
}

// flushTrace writes the stashed turn once, after later steps such as speech.
func (u *Utterance) flushTrace(tr *callTrace) {
	if u == nil || tr == nil {
		return
	}
	tr.mu.Lock()
	if !tr.has {
		tr.mu.Unlock()
		return
	}
	c := tr.rec
	calls := append([]domain.CallSpan(nil), tr.calls...)
	started := tr.started
	tr.has = false
	tr.mu.Unlock()

	end := time.Now()
	c.Started, c.Finished, c.MS = spanClock(started, end)
	if calls == nil {
		calls = []domain.CallSpan{}
	}
	c.Calls = calls
	if u.log != nil {
		u.log.Record(c)
	}
	applyCommand(tr.ctx, c)
}

// spanClock formats both instants and returns the milliseconds between them.
func spanClock(start, end time.Time) (string, string, int64) {
	startS := start.Format(time.RFC3339Nano)
	endS := end.Format(time.RFC3339Nano)
	s, errS := time.Parse(time.RFC3339Nano, startS)
	e, errE := time.Parse(time.RFC3339Nano, endS)
	if errS != nil || errE != nil {
		return startS, endS, end.Sub(start).Milliseconds()
	}
	return startS, endS, e.Sub(s).Milliseconds()
}
