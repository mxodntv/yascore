package usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"yascore/internal/domain"
)

type memBoard struct {
	levels []domain.SoundLevel
	sets   []int
}

func (m *memBoard) Levels(context.Context) []domain.SoundLevel {
	return append([]domain.SoundLevel(nil), m.levels...)
}

func (m *memBoard) SetLevel(_ context.Context, name string, percent int) error {
	m.sets = append(m.sets, percent)
	for i := range m.levels {
		if m.levels[i].Name == name {
			m.levels[i].Percent = percent
		}
	}
	return nil
}

func TestDuckerLowersActiveSkillAndRestores(t *testing.T) {
	board := &memBoard{levels: []domain.SoundLevel{
		{Name: "youtubemusic", Percent: 80, Active: true},
		{Name: "quiet", Percent: 40, Active: false},
	}}
	d := NewDucker(board)
	gen := d.Begin(context.Background())
	d.waitApply()
	if board.levels[0].Percent != 1 || len(board.sets) != 1 {
		t.Fatalf("after duck %+v sets %v", board.levels, board.sets)
	}
	again := d.Begin(context.Background())
	if again == gen || len(board.sets) != 1 {
		t.Fatalf("stacked duck sets %v gen %d/%d", board.sets, gen, again)
	}
	d.End(context.Background(), gen)
	if board.levels[0].Percent != 1 {
		t.Fatalf("old phrase restored early: %d", board.levels[0].Percent)
	}
	d.End(context.Background(), again)
	if board.levels[0].Percent != 80 || board.levels[1].Percent != 40 {
		t.Fatalf("restored %+v", board.levels)
	}
}

func TestDuckerCancelRestoresImmediately(t *testing.T) {
	board := &memBoard{levels: []domain.SoundLevel{{Name: "youtubemusic", Percent: 80, Active: true}}}
	d := NewDucker(board)
	gen := d.Begin(context.Background())
	d.waitApply()
	d.Cancel(context.Background())
	if board.levels[0].Percent != 80 {
		t.Fatalf("cancel left %d", board.levels[0].Percent)
	}
	d.End(context.Background(), gen)
	if board.levels[0].Percent != 80 {
		t.Fatalf("late end changed %d", board.levels[0].Percent)
	}
}

type failBoard struct {
	memBoard
	fail int
}

func (b *failBoard) SetLevel(ctx context.Context, name string, percent int) error {
	if b.fail > 0 {
		b.fail--
		return errors.New("player pipe")
	}
	return b.memBoard.SetLevel(ctx, name, percent)
}

func TestDuckerRetriesAFailedSetLevel(t *testing.T) {
	board := &failBoard{memBoard: memBoard{levels: []domain.SoundLevel{{Name: "youtubemusic", Percent: 80, Active: true}}}, fail: 1}
	d := NewDucker(board)
	d.Begin(context.Background())
	d.waitApply()
	if board.levels[0].Percent != 80 {
		t.Fatalf("failed write changed %d", board.levels[0].Percent)
	}
	d.Reapply(context.Background())
	if board.levels[0].Percent != 1 {
		t.Fatalf("retry %d", board.levels[0].Percent)
	}
}

func TestDuckerReapplyHoldsTheDuckedLevel(t *testing.T) {
	board := &memBoard{levels: []domain.SoundLevel{{Name: "youtubemusic", Percent: 80, Active: true}}}
	d := NewDucker(board)
	d.Begin(context.Background())
	d.waitApply()
	board.levels[0].Percent = 100
	d.Reapply(context.Background())
	if board.levels[0].Percent != 1 {
		t.Fatalf("reapply %d", board.levels[0].Percent)
	}
}

// The model is allowed to start while the player write is still blocked.
func TestBeginReturnsWhileThePlayerWriteBlocks(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	board := &blockBoard{
		memBoard: memBoard{levels: []domain.SoundLevel{{Name: "youtubemusic", Percent: 80, Active: true}}},
		entered:  entered,
		release:  release,
	}
	d := NewDucker(board)
	done := make(chan int, 1)
	go func() { done <- d.Begin(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("volume command was not started")
	}
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Begin waited for the player")
	}
	close(release)
	d.waitApply()
	if board.levels[0].Percent != 1 {
		t.Fatalf("after release %d", board.levels[0].Percent)
	}
}

type blockBoard struct {
	memBoard
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockBoard) SetLevel(ctx context.Context, name string, percent int) error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return b.memBoard.SetLevel(ctx, name, percent)
}
