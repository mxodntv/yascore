//go:build integration

package repo

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"yascore/internal/music"
)

// TestUserPausePersistsAndResumeSeeks stores a user pause in SQLite, then
// resume_only seeks back to that millisecond spot on a stubbed column.
func TestUserPausePersistsAndResumeSeeks(t *testing.T) {
	const playing = `{"state":{"status":"playing","position_ms":95025,"index":0,"queue":[{"id":"k9","title":"Монетка","artists":"ЛСП"}]}}`
	const paused = `{"state":{"status":"paused","position_ms":51,"index":0,"queue":[{"id":"k9","title":"Монетка","artists":"ЛСП"}]}}`

	store, err := music.Open(filepath.Join(t.TempDir(), "yascore.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var cmds []string
	pausedNow := false
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			cmds = append(cmds, remote)
			switch {
			case strings.Contains(remote, "status"):
				if pausedNow {
					return paused, nil
				}
				return playing, nil
			case strings.Contains(remote, " pause"):
				pausedNow = true
				return `{"ok":true}`, nil
			case strings.Contains(remote, " resume"):
				pausedNow = false
				return `{"ok":true}`, nil
			case strings.Contains(remote, " seek "):
				return `{"ok":true}`, nil
			default:
				return `{"ok":true}`, nil
			}
		},
	}
	p := NewMusicPlayer(col, musicService(t))
	p.UseMemory(music.PlayerMemory{Store: store})

	if _, err := p.Control(context.Background(), "pause"); err != nil {
		t.Fatal(err)
	}
	_, ms, ok, err := store.LoadPlayback(music.MediaMusic)
	if err != nil || !ok || ms != 95025 {
		t.Fatalf("stored pause ms=%d ok=%v err=%v", ms, ok, err)
	}

	cmds = nil
	if _, err := p.ApplyIntent(context.Background(), `{"do":"resume_only"}`); err != nil {
		t.Fatal(err)
	}
	sawSeek := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, " seek 95") {
			sawSeek = true
		}
	}
	if !sawSeek {
		t.Fatalf("expected seek 95 in %v", cmds)
	}
}
