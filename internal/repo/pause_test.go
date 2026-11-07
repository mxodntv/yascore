package repo

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"yascore/internal/domain"
)

func TestPauseAllReachesSoundSkillsOnly(t *testing.T) {
	hub := NewHub()
	music := newChanConn()
	radio := newChanConn()
	search := newChanConn()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = hub.Accept(ctx, music) }()
	go func() { _ = hub.Accept(ctx, radio) }()
	go func() { _ = hub.Accept(ctx, search) }()

	sendReg(t, music, domain.BusMessage{Type: "register", Name: "youtubemusic", Sound: true})
	sendReg(t, radio, domain.BusMessage{Type: "register", Name: "radio", Sound: true})
	sendReg(t, search, domain.BusMessage{
		Type: "register", Name: "websearch",
		Tools: []domain.Tool{{Name: "web_search", Description: "поиск"}},
	})
	waitAck(t, music)
	waitAck(t, radio)
	waitAck(t, search)

	musicLog := collectBus(ctx, music)
	radioLog := collectBus(ctx, radio)
	searchLog := collectBus(ctx, search)

	hub.PauseAll(ctx)

	if !musicLog.wait("pause") || !radioLog.wait("pause") {
		t.Fatalf("sound skills music=%v radio=%v", musicLog.snapshot(), radioLog.snapshot())
	}
	if searchLog.has("pause") {
		t.Fatalf("non-sound skill got %v", searchLog.snapshot())
	}
}

func TestHubResumeReachesMediaSkillOnly(t *testing.T) {
	hub := NewHub()
	music := newChanConn()
	weather := newChanConn()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = hub.Accept(ctx, music) }()
	go func() { _ = hub.Accept(ctx, weather) }()
	sendReg(t, music, domain.BusMessage{
		Type: "register", Name: "youtubemusic", Sound: true, Media: true,
		Tools: []domain.Tool{{Name: "play_music"}},
	})
	sendReg(t, weather, domain.BusMessage{
		Type: "register", Name: "weather",
		Tools: []domain.Tool{{Name: "weather"}},
	})
	waitAck(t, music)
	waitAck(t, weather)

	weatherLog := collectBus(ctx, weather)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case raw, ok := <-music.out:
				if !ok {
					return
				}
				var msg domain.BusMessage
				if json.Unmarshal(raw, &msg) != nil || msg.ID == "" {
					continue
				}
				content := "ok"
				if msg.Type == "level" {
					content = `{"percent":80,"active":true}`
				}
				if msg.Type == "resume" {
					content = domain.SilentReply
				}
				body, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: content})
				select {
				case music.in <- body:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	if name, ok := hub.MediaSkill("play_music"); !ok || name != "youtubemusic" {
		t.Fatalf("media skill %q %v", name, ok)
	}
	if _, ok := hub.MediaSkill("weather"); ok {
		t.Fatal("weather is media")
	}
	if !hub.Playing(ctx) {
		t.Fatal("media skill was not playing")
	}
	if _, err := hub.Resume(ctx, "weather"); err == nil {
		t.Fatal("resumed an ordinary skill")
	}
	got, err := hub.Resume(ctx, "youtubemusic")
	if err != nil || got != domain.SilentReply {
		t.Fatalf("resume %q %v", got, err)
	}
	if weatherLog.has("resume") || weatherLog.has("level") {
		t.Fatalf("weather saw %v", weatherLog.snapshot())
	}
}

func sendReg(t *testing.T, conn *chanConn, msg domain.BusMessage) {
	t.Helper()
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	conn.in <- raw
}

func waitAck(t *testing.T, conn *chanConn) {
	t.Helper()
	select {
	case raw := <-conn.out:
		var msg domain.BusMessage
		if err := json.Unmarshal(raw, &msg); err != nil || msg.Type != "registered" {
			t.Fatalf("ack %s", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("no ack")
	}
}

type busLog struct {
	mu    sync.Mutex
	types []string
}

func collectBus(ctx context.Context, conn *chanConn) *busLog {
	log := &busLog{}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case raw, ok := <-conn.out:
				if !ok {
					return
				}
				var msg domain.BusMessage
				if json.Unmarshal(raw, &msg) != nil {
					continue
				}
				log.mu.Lock()
				log.types = append(log.types, msg.Type)
				log.mu.Unlock()
				if msg.ID == "" {
					continue
				}
				body, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: "ok"})
				select {
				case conn.in <- body:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return log
}

func (l *busLog) wait(want string) bool {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if l.has(want) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return l.has(want)
}

func (l *busLog) has(want string) bool {
	for _, got := range l.snapshot() {
		if got == want {
			return true
		}
	}
	return false
}

func (l *busLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.types...)
}
