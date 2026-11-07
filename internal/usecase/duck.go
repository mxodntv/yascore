package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

	"yascore/config"
	"yascore/internal/domain"
)

// Ducker lowers every playing skill to core.duck.percent of its level and restores that level
// after the matching speech ends. A second phrase does not duck the already ducked level.
// Begin returns as soon as the volume write is started. The player round trip
// runs beside the model, not before it.
type Ducker struct {
	board   domain.SoundBoard
	mu      sync.Mutex
	gen     int
	session int
	open    bool
	saved   []domain.SoundLevel
	applyWG sync.WaitGroup
}

// NewDucker returns a ducker. A nil board ducks nothing.
func NewDucker(board domain.SoundBoard) *Ducker {
	return &Ducker{board: board}
}

// Begin ducks active skills and returns a generation. End restores only the latest one.
// The generation is returned once the volume write has been started. The caller
// can ask the model without waiting for the player to answer.
func (d *Ducker) Begin(ctx context.Context) int {
	if d == nil || d.board == nil {
		return 0
	}
	d.mu.Lock()
	d.gen++
	gen := d.gen
	first := !d.open
	d.open = true
	sess := d.session
	if first {
		d.applyWG.Add(1)
	}
	d.mu.Unlock()
	if !first {
		NoteAction(ctx, "duck", "begin", "already", time.Now(), time.Now(), nil)
		return gen
	}
	started := make(chan struct{})
	go func() {
		defer d.applyWG.Done()
		d.apply(context.WithoutCancel(ctx), sess, started)
	}()
	select {
	case <-started:
	case <-ctx.Done():
	}
	return gen
}

// apply reads levels, then writes the ducked gain. started is closed before
// the player write so Begin can return while that write is still running.
func (d *Ducker) apply(ctx context.Context, sess int, started chan struct{}) {
	var once sync.Once
	signal := func() { once.Do(func() { close(started) }) }
	defer signal()

	var saved []domain.SoundLevel
	for _, lv := range d.board.Levels(ctx) {
		if !lv.Active || lv.Name == "" {
			continue
		}
		saved = append(saved, lv)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.open || d.session != sess {
		return
	}
	if len(saved) == 0 {
		d.open = false
		NoteAction(ctx, "duck", "begin", "none", time.Now(), time.Now(), nil)
		return
	}
	d.saved = saved
	signal()
	for _, lv := range saved {
		if !d.open || d.session != sess {
			return
		}
		// A failed write is kept. The player ignores one line, and the next
		// reapply has to try the same ducked level again.
		pct := domain.DuckPercent(lv.Percent)
		start := time.Now()
		err := d.board.SetLevel(ctx, lv.Name, pct)
		NoteAction(ctx, "duck", "begin", fmt.Sprintf("%s %d%%", lv.Name, pct), start, time.Now(), err)
		if err != nil {
			config.Error("duck", "skill", lv.Name, "err", err)
		}
	}
}

// waitApply blocks until the volume write started by Begin has finished.
func (d *Ducker) waitApply() {
	if d == nil {
		return
	}
	d.applyWG.Wait()
}

// Reapply writes the ducked level again. The player resets its own gain when
// the column leaves the dialog, so one write does not last through the phrase.
// The lock is held across the write so a restore cannot be overwritten afterwards.
func (d *Ducker) Reapply(ctx context.Context) {
	if d == nil || d.board == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.open {
		return
	}
	for _, lv := range d.saved {
		_ = d.board.SetLevel(ctx, lv.Name, domain.DuckPercent(lv.Percent))
	}
}

// Cancel restores ducked levels immediately, under the same lock as End.
// A later End for the old phrase does nothing. A failed SetLevel keeps that
// level so a later restore can retry.
func (d *Ducker) Cancel(ctx context.Context) {
	if d == nil || d.board == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gen++
	d.session++
	d.open = false
	d.restore(ctx)
}

// End restores the levels captured by Begin when gen is still the latest phrase.
func (d *Ducker) End(ctx context.Context, gen int) {
	if d == nil || gen == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if gen != d.gen {
		NoteAction(ctx, "duck", "end", "stale", time.Now(), time.Now(), nil)
		return
	}
	if !d.open {
		return
	}
	d.session++
	d.open = false
	d.restore(ctx)
}

// restore writes saved levels back. Failures stay in saved.
// Caller holds d.mu.
func (d *Ducker) restore(ctx context.Context) {
	if len(d.saved) == 0 {
		return
	}
	var kept []domain.SoundLevel
	for _, lv := range d.saved {
		start := time.Now()
		err := d.board.SetLevel(ctx, lv.Name, lv.Percent)
		NoteAction(ctx, "duck", "end", fmt.Sprintf("%s %d%%", lv.Name, lv.Percent), start, time.Now(), err)
		if err != nil {
			kept = append(kept, lv)
		}
	}
	if len(kept) == 0 {
		d.saved = nil
		return
	}
	d.saved = kept
}
