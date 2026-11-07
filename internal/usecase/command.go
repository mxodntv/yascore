package usecase

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"yascore/internal/domain"
)

// CommandScope collects the steps of one command and writes them once.
// A nested OpenCommand shares the outer scope and does not write early.
type CommandScope struct {
	inner *commandScope
	owned bool
}

type commandScope struct {
	mu       sync.Mutex
	logger   domain.CommandLogger
	source   string
	heard    string
	user     string
	outcome  string
	started  time.Time
	actions  []domain.CommandAction
	finished bool
}

type commandKey struct{}

// OpenCommand starts a command record on ctx.
// A nil logger, or a scope already on ctx, does not write a second line.
func OpenCommand(ctx context.Context, source string, log domain.CommandLogger) (context.Context, *CommandScope) {
	if ctx == nil {
		ctx = context.Background()
	}
	if existing := scopeFrom(ctx); existing != nil {
		return ctx, &CommandScope{inner: existing, owned: false}
	}
	if log == nil {
		return ctx, &CommandScope{}
	}
	inner := &commandScope{
		logger:  log,
		source:  source,
		started: time.Now(),
	}
	return context.WithValue(ctx, commandKey{}, inner), &CommandScope{inner: inner, owned: true}
}

// Finish writes the record. A nested scope, or a second call, does not write again.
func (s *CommandScope) Finish() {
	if s == nil || s.inner == nil || !s.owned {
		return
	}
	s.inner.finish()
}

// SetOutcome keeps the latest non-empty result for this command.
func (s *CommandScope) SetOutcome(outcome string) {
	setOutcome(contextWith(s), outcome)
}

func contextWith(s *CommandScope) context.Context {
	if s == nil || s.inner == nil {
		return context.Background()
	}
	return context.WithValue(context.Background(), commandKey{}, s.inner)
}

func scopeFrom(ctx context.Context) *commandScope {
	if ctx == nil {
		return nil
	}
	sc, _ := ctx.Value(commandKey{}).(*commandScope)
	return sc
}

// NoteAction appends one step that actually ran. A failed step is kept.
// Nothing is written when this context has no open command.
func NoteAction(ctx context.Context, name, by, detail string, start, end time.Time, err error) {
	sc := scopeFrom(ctx)
	if sc == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "call"
	}
	by = strings.TrimSpace(by)
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = name
	}
	action := domain.NewAction(name, by, detail, start, end, err)
	sc.mu.Lock()
	sc.actions = append(sc.actions, action)
	sc.mu.Unlock()
}

func rememberHeard(ctx context.Context, heard, user string) {
	sc := scopeFrom(ctx)
	if sc == nil {
		return
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if strings.TrimSpace(sc.heard) == "" {
		sc.heard = heard
	}
	if strings.TrimSpace(user) != "" {
		sc.user = user
	}
}

func setOutcome(ctx context.Context, outcome string) {
	sc := scopeFrom(ctx)
	if sc == nil {
		return
	}
	outcome = strings.TrimSpace(outcome)
	if outcome == "" {
		return
	}
	sc.mu.Lock()
	sc.outcome = outcome
	sc.mu.Unlock()
}

func (u *Utterance) openCommand(ctx context.Context, source string) (context.Context, *CommandScope) {
	var log domain.CommandLogger
	if u != nil {
		log = u.commands
	}
	return OpenCommand(ctx, source, log)
}

func applyCommand(ctx context.Context, c domain.Conversation) {
	sc := scopeFrom(ctx)
	if sc == nil {
		return
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if strings.TrimSpace(c.User) != "" {
		sc.user = c.User
	}
	if out := outcomeFromConv(c); out != "" {
		sc.outcome = out
	}
}

func outcomeFromReply(reply domain.Reply, err error) string {
	if reply.Text == domain.EchoReply {
		return "ignored: echo"
	}
	if err != nil {
		return err.Error()
	}
	if reply.Quiet && (strings.TrimSpace(reply.Text) == "" || reply.Text == domain.SilentReply) {
		return "quiet"
	}
	if strings.TrimSpace(reply.Text) != "" {
		return reply.Text
	}
	if reply.Quiet {
		return "quiet"
	}
	return ""
}

func outcomeFromConv(c domain.Conversation) string {
	var err error
	if strings.TrimSpace(c.Error) != "" {
		err = errString(c.Error)
	}
	return outcomeFromReply(domain.Reply{Text: c.Reply, Quiet: c.Quiet}, err)
}

type errString string

func (e errString) Error() string { return string(e) }

func (sc *commandScope) finish() {
	if sc == nil || sc.logger == nil {
		return
	}
	sc.mu.Lock()
	if sc.finished {
		sc.mu.Unlock()
		return
	}
	sc.finished = true
	actions := append([]domain.CommandAction(nil), sc.actions...)
	rec := domain.Command{
		Source:  sc.source,
		Heard:   sc.heard,
		User:    sc.user,
		Outcome: sc.outcome,
		Started: sc.started.Format(time.RFC3339Nano),
	}
	started := sc.started
	sc.mu.Unlock()

	sort.SliceStable(actions, func(i, j int) bool {
		return actions[i].Start < actions[j].Start
	})
	if actions == nil {
		actions = []domain.CommandAction{}
	}
	end := time.Now()
	_, finished, ms := domain.Span(started, end)
	rec.Finished = finished
	rec.MS = ms
	rec.Actions = actions
	sc.logger.Record(rec)
}
