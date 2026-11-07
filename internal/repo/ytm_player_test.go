package repo

import (
	"context"
	"strings"
	"testing"

	"yascore/config"
	"yascore/internal/domain"
)

func musicService(t *testing.T) string {
	t.Helper()
	cfg, err := config.LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Skills.YouTubeMusic.ServiceURL
}

func TestPauseStoresPositionAndResumeSeeks(t *testing.T) {
	const playing = `{"state":{"status":"playing","position_ms":95025,"index":0,"queue":[{"id":"k9","title":"Монетка","artists":"ЛСП"}]}}`
	const paused = `{"state":{"status":"paused","position_ms":95025,"index":0,"queue":[{"id":"k9","title":"Монетка","artists":"ЛСП"}]}}`
	var cmds []string
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			cmds = append(cmds, remote)
			if strings.Contains(remote, "status") {
				return playing, nil
			}
			return "ok", nil
		},
	}
	p := NewYTMPlayer(col, musicService(t))
	pos, err := p.PausePlaying(context.Background())
	if err != nil || pos != 95025 {
		t.Fatalf("pause %d %v", pos, err)
	}
	var pausedOnce bool
	for _, cmd := range cmds {
		if strings.Contains(cmd, " pause") {
			pausedOnce = true
		}
	}
	if !pausedOnce {
		t.Fatalf("no pause in %v", cmds)
	}

	cmds = nil
	col.RunRemote = func(_ context.Context, remote string) (string, error) {
		cmds = append(cmds, remote)
		if strings.Contains(remote, "status") {
			return paused, nil
		}
		if strings.Contains(remote, " play ") {
			t.Fatalf("resumed from the start: %s", remote)
		}
		return `{"state":{"status":"playing","position_ms":95025}}`, nil
	}
	if err := p.ResumePlaying(context.Background(), 95025); err != nil {
		t.Fatal(err)
	}
	var sawResume bool
	for _, cmd := range cmds {
		if strings.Contains(cmd, " seek ") {
			t.Fatalf("unnecessary seek %v", cmds)
		}
		if strings.Contains(cmd, " resume") {
			sawResume = true
		}
	}
	if !sawResume {
		t.Fatalf("resume cmds %v", cmds)
	}
}

func TestResumeSeeksAfterResumeWhenPositionWasLost(t *testing.T) {
	const pausedNearStart = `{"state":{"status":"paused","position_ms":51,"index":0,"queue":[{"id":"k9","title":"Монетка","artists":"ЛСП"}]}}`
	var cmds []string
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			cmds = append(cmds, remote)
			if strings.Contains(remote, "status") {
				return pausedNearStart, nil
			}
			return "ok", nil
		},
	}
	p := NewYTMPlayer(col, musicService(t))
	if err := p.ResumePlaying(context.Background(), 95000); err != nil {
		t.Fatal(err)
	}
	var sawResume, sawSeek bool
	resumeAt, seekAt := -1, -1
	for i, cmd := range cmds {
		if strings.Contains(cmd, " resume") {
			sawResume = true
			resumeAt = i
		}
		if strings.Contains(cmd, " seek 95") {
			sawSeek = true
			seekAt = i
		}
	}
	if !sawResume || !sawSeek || seekAt < resumeAt {
		t.Fatalf("expected resume then seek 95, got %v", cmds)
	}
}

func TestApplySearchPlayIntent(t *testing.T) {
	var cmds []string
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			cmds = append(cmds, remote)
			if strings.Contains(remote, "search") {
				return `{"tracks":[{"id":"abc","title":"Get Lucky","artists":"Daft Punk"}]}`, nil
			}
			if strings.Contains(remote, "play") {
				return `{"state":{"status":"playing","index":0,"queue":[{"id":"abc","title":"Get Lucky","artists":"Daft Punk"}]}}`, nil
			}
			return "ok", nil
		},
	}
	p := NewYTMPlayer(col, musicService(t))
	raw := `{"do":"search_play","query":"Get Lucky"}`
	got, err := p.ApplyIntent(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "играет Get Lucky") {
		t.Fatalf("got %q", got)
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "search") || !strings.Contains(joined, "watch?v=abc") {
		t.Fatalf("cmds %v", cmds)
	}
}

func TestPausePlayingSkipsAlreadyStopped(t *testing.T) {
	var cmds []string
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			cmds = append(cmds, remote)
			return `{"state":{"status":"stopped","position_ms":12000,"queue":[{"title":"X"}]}}`, nil
		},
	}
	p := NewYTMPlayer(col, musicService(t))
	pos, err := p.PausePlaying(context.Background())
	if err != nil || pos != -1 {
		t.Fatalf("pos %d err %v", pos, err)
	}
	for _, cmd := range cmds {
		if strings.Contains(cmd, " pause") || strings.Contains(cmd, " resume") {
			t.Fatalf("must not touch a stopped player: %v", cmds)
		}
	}
}

func TestControlNextIsDirect(t *testing.T) {
	var cmds []string
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			cmds = append(cmds, remote)
			return "ok", nil
		},
	}
	p := NewYTMPlayer(col, musicService(t))
	got, err := p.Control(context.Background(), "next")
	if err != nil || got != domain.SilentReply {
		t.Fatalf("%q %v", got, err)
	}
	if len(cmds) != 1 || !strings.Contains(cmds[0], " next") {
		t.Fatalf("cmds %v", cmds)
	}
}
