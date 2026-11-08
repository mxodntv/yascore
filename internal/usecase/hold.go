package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"yascore/internal/domain"
)

// Quiet windows after a wake turn. A follow-up during the wait cancels resume.
// ConfigureHold installs them from the loaded config.
var (
	resumeQuiet         time.Duration
	resumeAfterQuestion time.Duration
)

// ConfigureHold installs the post-reply waits from the loaded config.
func ConfigureHold(quiet, afterQuestion time.Duration) {
	resumeQuiet = quiet
	resumeAfterQuestion = afterQuestion
}

// mediaPauser pauses playback and returns the millisecond spot to resume from.
// Volume is not changed. -1 means the skill did not report a spot.
type mediaPauser interface {
	PausePlaying(ctx context.Context) (int, error)
	ResumePlaying(ctx context.Context, positionMS int) error
}

type wakeNamer interface {
	Words() []string
}

// woke reports that the accepted line carries the configured wake word.
func (u *Utterance) woke(raw string) bool {
	if u == nil {
		return false
	}
	if u.Addressed(raw) {
		return true
	}
	if namer, ok := u.wake.(wakeNamer); ok {
		return domain.Heard(raw, namer.Words())
	}
	return false
}

// pauseOnWake stops every media skill before tools and TTS.
// The spot is kept so the quiet window can resume the same place.
func (u *Utterance) pauseOnWake(ctx context.Context) {
	if u == nil || u.skills == nil {
		return
	}
	p, ok := u.skills.(mediaPauser)
	if !ok || p == nil {
		// PauseAll still stops sound when the hub has no position API.
		// Without a playing spot there is nothing to auto-resume later.
		if pause, ok := u.skills.(pauser); ok && pause != nil {
			start := time.Now()
			err := pause.PauseAll(ctx)
			NoteAction(ctx, "pause", "wake", "pause-all", start, time.Now(), err)
		}
		u.clearMediaHold()
		return
	}
	start := time.Now()
	pos, err := p.PausePlaying(ctx)
	NoteAction(ctx, "pause", "wake", fmtPosition(pos), start, time.Now(), err)
	if pos >= 0 {
		u.armMediaHold(pos)
		return
	}
	// Already quiet: leave it quiet after the dialogue.
	u.clearMediaHold()
}

func fmtPosition(pos int) string {
	return fmt.Sprintf("position_ms=%d", pos)
}

func (u *Utterance) armMediaHold(pos int) {
	u.holdMu.Lock()
	defer u.holdMu.Unlock()
	u.holdGen++
	u.holdPos = pos
	u.holding = true
}

// cancelMediaResume drops a pending resume. The player stays paused.
func (u *Utterance) cancelMediaResume() {
	if u == nil {
		return
	}
	u.holdMu.Lock()
	defer u.holdMu.Unlock()
	if u.holding {
		u.holdGen++
	}
}

func (u *Utterance) clearMediaHold() {
	if u == nil {
		return
	}
	u.holdMu.Lock()
	defer u.holdMu.Unlock()
	u.holding = false
	u.holdGen++
	u.holdWait = 0
}

func (u *Utterance) mediaHolding() bool {
	if u == nil {
		return false
	}
	u.holdMu.Lock()
	defer u.holdMu.Unlock()
	return u.holding
}

func (u *Utterance) quietFor(reply domain.Reply) time.Duration {
	if reply.Instant {
		return 0
	}
	ask := resumeAfterQuestion
	quiet := resumeQuiet
	if u != nil {
		if u.holdAsk > 0 {
			ask = u.holdAsk
		}
		if u.holdQuiet > 0 {
			quiet = u.holdQuiet
		}
	}
	if keepsOpen(reply.Text) {
		return ask
	}
	return quiet
}

// SetHoldQuiet shortens the post-reply resume wait in tests.
func (u *Utterance) SetHoldQuiet(d time.Duration) {
	if u == nil {
		return
	}
	u.holdQuiet = d
}

// SetHoldAsk shortens the post-question resume wait in tests.
func (u *Utterance) SetHoldAsk(d time.Duration) {
	if u == nil {
		return
	}
	u.holdAsk = d
}

func keepsOpen(reply string) bool {
	s := strings.TrimSpace(reply)
	return strings.HasSuffix(s, "?") || strings.HasSuffix(s, "？")
}

// scheduleMediaResume continues the paused track after the quiet window
// when no newer command arrived.
func (u *Utterance) scheduleMediaResume(wait time.Duration) {
	if u == nil || !u.mediaHolding() {
		return
	}
	if wait < 0 {
		wait = 0
	}
	u.holdMu.Lock()
	u.holdGen++
	gen := u.holdGen
	u.holdWait = wait
	pos := u.holdPos
	u.holdMu.Unlock()
	time.AfterFunc(wait, func() { u.finishMediaHold(gen, pos) })
}

// extendMediaHold pushes the resume deadline past speech still on the column.
func (u *Utterance) extendMediaHold(extra time.Duration) {
	if u == nil || extra <= 0 || !u.mediaHolding() {
		return
	}
	u.holdMu.Lock()
	wait := u.holdWait
	u.holdMu.Unlock()
	if wait <= 0 {
		wait = resumeQuiet
		if u.holdQuiet > 0 {
			wait = u.holdQuiet
		}
	}
	u.scheduleMediaResume(extra + wait)
}

func (u *Utterance) finishMediaHold(gen, pos int) {
	if u == nil {
		return
	}
	u.holdMu.Lock()
	if !u.holding || u.holdGen != gen {
		u.holdMu.Unlock()
		return
	}
	u.holding = false
	u.holdGen++
	u.holdWait = 0
	skills := u.skills
	u.holdMu.Unlock()
	p, ok := skills.(mediaPauser)
	if !ok || p == nil {
		return
	}
	_ = p.ResumePlaying(context.Background(), pos)
}
