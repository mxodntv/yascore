package repo

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"yascore/config"
	"yascore/internal/domain"
)

func musicService(t *testing.T) string {
	t.Helper()
	cfg, err := config.LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Skills.MusicYouTube.ServiceURL
}

func TestUserPauseWritesMemory(t *testing.T) {
	const playing = `{"state":{"status":"playing","position_ms":95025,"index":0,"queue":[{"id":"k9","title":"Монетка","artists":"ЛСП"}]}}`
	mem := &fakeMusicMemory{}
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			if strings.Contains(remote, "status") {
				return playing, nil
			}
			return "ok", nil
		},
	}
	p := NewMusicPlayer(col, musicService(t))
	p.UseMemory(mem)
	if _, err := p.Control(context.Background(), "pause"); err != nil {
		t.Fatal(err)
	}
	if mem.ms != 95025 || mem.title != "Монетка" || mem.videoID != "k9" {
		t.Fatalf("memory %+v", mem)
	}
}

type fakeMusicMemory struct {
	title, artist, videoID string
	ms                     int
	saved                  bool
	starts                 int
	finishes               int
	queued                 int
	lastAppendQueue        bool
	lastFinishMS           int
}

func (m *fakeMusicMemory) SavePlayback(_ string, title, artist, videoID string, positionMS int) error {
	m.title, m.artist, m.videoID, m.ms, m.saved = title, artist, videoID, positionMS, true
	return nil
}
func (m *fakeMusicMemory) LoadPlayback(string) (string, string, string, int, bool, error) {
	if !m.saved {
		return "", "", "", -1, false, nil
	}
	return m.title, m.artist, m.videoID, m.ms, true, nil
}
func (m *fakeMusicMemory) ClearPlayback(string) error { m.saved = false; return nil }
func (m *fakeMusicMemory) RandomLike() (string, string, string, bool, error) {
	return "", "", "", false, nil
}
func (m *fakeMusicMemory) RecordStart(_ string, _ string, _ string, _ string, _ time.Time, appendQueue bool) error {
	m.starts++
	m.lastAppendQueue = appendQueue
	return nil
}
func (m *fakeMusicMemory) FinishListen(ms int) error {
	m.finishes++
	m.lastFinishMS = ms
	return nil
}
func (m *fakeMusicMemory) EnsureQueued(titles, artists, externalIDs []string, _ string) error {
	m.queued = len(titles)
	if len(artists) < m.queued {
		m.queued = len(artists)
	}
	if len(externalIDs) < m.queued {
		m.queued = len(externalIDs)
	}
	return nil
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
	p := NewMusicPlayer(col, musicService(t))
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
	p := NewMusicPlayer(col, musicService(t))
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
	p := NewMusicPlayer(col, musicService(t))
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
	p := NewMusicPlayer(col, musicService(t))
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
	const nextTrack = `{"state":{"status":"playing","position_ms":1000,"index":1,"queue":[{"id":"a","title":"A","artists":"X"},{"id":"b","title":"B","artists":"Y"}]}}`
	var cmds []string
	mem := &fakeMusicMemory{}
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			cmds = append(cmds, remote)
			if strings.Contains(remote, "status") {
				return nextTrack, nil
			}
			return "ok", nil
		},
	}
	p := NewMusicPlayer(col, musicService(t))
	p.UseMemory(mem)
	got, err := p.Control(context.Background(), "next")
	if err != nil || got != domain.SilentReply {
		t.Fatalf("%q %v", got, err)
	}
	var sawNext bool
	for _, cmd := range cmds {
		if strings.Contains(cmd, " next") {
			sawNext = true
		}
	}
	if !sawNext {
		t.Fatalf("cmds %v", cmds)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if mem.starts >= 1 && mem.finishes >= 1 && mem.lastAppendQueue {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("memory starts=%d append=%v finishes=%d", mem.starts, mem.lastAppendQueue, mem.finishes)
}

func TestStartPhraseRecordsWithoutForcingQueue(t *testing.T) {
	const playing = `{"state":{"status":"playing","position_ms":500,"index":0,"queue":[{"id":"abc","title":"Get Lucky","artists":"Daft Punk"}]}}`
	mem := &fakeMusicMemory{}
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			if strings.Contains(remote, "status") || strings.Contains(remote, "play") {
				return playing, nil
			}
			return "ok", nil
		},
	}
	p := NewMusicPlayer(col, musicService(t))
	p.UseMemory(mem)
	raw := `{"do":"search_play","query":"Get Lucky"}`
	if _, err := p.ApplyIntent(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if mem.starts != 1 || mem.lastAppendQueue {
		t.Fatalf("starts=%d appendQueue=%v", mem.starts, mem.lastAppendQueue)
	}
}

func TestPauseFinishesListen(t *testing.T) {
	const playing = `{"state":{"status":"playing","position_ms":95025,"index":0,"queue":[{"id":"k9","title":"Монетка","artists":"ЛСП"}]}}`
	mem := &fakeMusicMemory{}
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			if strings.Contains(remote, "status") {
				return playing, nil
			}
			return "ok", nil
		},
	}
	p := NewMusicPlayer(col, musicService(t))
	p.UseMemory(mem)
	if _, err := p.Control(context.Background(), "pause"); err != nil {
		t.Fatal(err)
	}
	if mem.finishes != 1 || mem.lastFinishMS != 95025 {
		t.Fatalf("finish finishes=%d ms=%d", mem.finishes, mem.lastFinishMS)
	}
}

func TestNeighborIDsWindow(t *testing.T) {
	queue := []musicTrack{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}, {ID: "f"},
	}
	behind, forward := neighborIDs(queue, 2, 2, 3)
	if strings.Join(behind, ",") != "b,a" {
		t.Fatalf("behind %v", behind)
	}
	if strings.Join(forward, ",") != "d,e,f" {
		t.Fatalf("forward %v", forward)
	}
}

func TestPrefetchDeferredCacheOnly(t *testing.T) {
	const status = `{"state":{"status":"playing","index":2,"queue":[
		{"id":"a","title":"A"},{"id":"b","title":"B"},{"id":"c","title":"C"},
		{"id":"d","title":"D"},{"id":"e","title":"E"},{"id":"f","title":"F"}
	]}}`
	var mu sync.Mutex
	var cmds []string
	done := make(chan struct{})
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			mu.Lock()
			cmds = append(cmds, remote)
			n := 0
			for _, c := range cmds {
				if strings.Contains(c, "cache add") {
					n++
				}
			}
			mu.Unlock()
			if n >= 5 { // 3 forward + 2 behind; no playlist add
				select {
				case <-done:
				default:
					close(done)
				}
			}
			return "ok", nil
		},
	}
	p := NewMusicPlayer(col, musicService(t))
	p.prefetchDelay = 20 * time.Millisecond
	p.prefetchGap = time.Millisecond
	p.schedulePrefetch(status)
	mu.Lock()
	early := len(cmds)
	mu.Unlock()
	if early != 0 {
		t.Fatalf("prefetch must wait, cmds=%d", early)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		mu.Lock()
		got := append([]string(nil), cmds...)
		mu.Unlock()
		t.Fatalf("timeout cmds %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	var cache, add int
	for _, c := range cmds {
		if strings.Contains(c, "cache add") {
			cache++
		} else if strings.Contains(c, " add ") {
			add++
		}
	}
	if cache != 5 || add != 0 {
		t.Fatalf("cache=%d add=%d cmds %v", cache, add, cmds)
	}
}

func TestPrefetchReschedulesOnNewerGen(t *testing.T) {
	const first = `{"state":{"status":"playing","index":0,"queue":[
		{"id":"a","title":"A"},{"id":"b","title":"B"},{"id":"c","title":"C"}
	]}}`
	const second = `{"state":{"status":"playing","index":1,"queue":[
		{"id":"a","title":"A"},{"id":"b","title":"B"},{"id":"c","title":"C"},{"id":"d","title":"D"}
	]}}`
	var mu sync.Mutex
	var cmds []string
	done := make(chan struct{})
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			mu.Lock()
			cmds = append(cmds, remote)
			sawD := false
			for _, c := range cmds {
				if strings.Contains(c, "cache add") && strings.Contains(c, "d") {
					sawD = true
				}
			}
			mu.Unlock()
			if sawD {
				select {
				case <-done:
				default:
					close(done)
				}
			}
			return "ok", nil
		},
	}
	p := NewMusicPlayer(col, musicService(t))
	p.prefetchDelay = 80 * time.Millisecond
	p.prefetchGap = time.Millisecond
	p.schedulePrefetch(first)
	time.Sleep(20 * time.Millisecond)
	p.schedulePrefetch(second) // should cancel first; warm around index 1 (incl. d)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		mu.Lock()
		got := append([]string(nil), cmds...)
		mu.Unlock()
		t.Fatalf("timeout waiting for d cache, cmds %v", got)
	}
}

func TestControlNextReturnsBeforeStatus(t *testing.T) {
	statusGate := make(chan struct{})
	var mu sync.Mutex
	var cmds []string
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			mu.Lock()
			cmds = append(cmds, remote)
			mu.Unlock()
			if strings.Contains(remote, "status") {
				<-statusGate
				return `{"state":{"status":"playing","index":1,"queue":[{"id":"a","title":"A"},{"id":"b","title":"B"}]}}`, nil
			}
			return "ok", nil
		},
	}
	p := NewMusicPlayer(col, musicService(t))
	p.UseMemory(&fakeMusicMemory{})
	done := make(chan error, 1)
	go func() {
		_, err := p.Control(context.Background(), "next")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("next blocked on status")
	}
	close(statusGate)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(cmds)
		mu.Unlock()
		if n >= 2 { // next + later status
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWatchRecordsAutoTrackSwitch(t *testing.T) {
	const first = `{"state":{"status":"playing","position_ms":1000,"index":0,"queue":[{"id":"a","title":"A","artists":"X"}]}}`
	const second = `{"state":{"status":"playing","position_ms":200,"index":1,"queue":[{"id":"a","title":"A","artists":"X"},{"id":"b","title":"B","artists":"Y"}]}}`
	mem := &fakeMusicMemory{}
	status := first
	col := &Column{
		Bin: "/bin/ytm",
		RunRemote: func(_ context.Context, remote string) (string, error) {
			if strings.Contains(remote, "status") {
				return status, nil
			}
			return "ok", nil
		},
	}
	p := NewMusicPlayer(col, musicService(t))
	p.UseMemory(mem)
	p.armFromStatus(first)
	p.mu.Lock()
	p.follow.heard = true
	p.mu.Unlock()

	status = second
	if err := p.observe(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if mem.starts != 1 || !mem.lastAppendQueue || mem.finishes < 1 {
		t.Fatalf("starts=%d append=%v finishes=%d", mem.starts, mem.lastAppendQueue, mem.finishes)
	}
	p.mu.Lock()
	cur := p.follow.current
	armed := p.follow.armed
	p.mu.Unlock()
	if !armed || cur.Title != "B" {
		t.Fatalf("follow %+v armed=%v", cur, armed)
	}
}
