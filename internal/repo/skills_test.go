package repo

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"yascore/internal/domain"
)

type chanConn struct {
	in  chan []byte
	out chan []byte
}

func newChanConn() *chanConn {
	return &chanConn{in: make(chan []byte, 4), out: make(chan []byte, 4)}
}

func (c *chanConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case b, ok := <-c.in:
		if !ok {
			return nil, io.EOF
		}
		return b, nil
	}
}

func (c *chanConn) Write(ctx context.Context, p []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.out <- append([]byte(nil), p...):
		return nil
	}
}

func (c *chanConn) Close() error {
	close(c.in)
	return nil
}

func TestHubRegisterInvokeDrop(t *testing.T) {
	hub := NewHub()
	conn := newChanConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = hub.Accept(ctx, conn)
		close(done)
	}()
	reg, _ := json.Marshal(domain.BusMessage{
		Type: "register", Name: "websearch", Hints: []string{"найди"},
		Tools: []domain.Tool{{Name: "web_search", Description: "поиск"}},
	})
	conn.in <- reg
	select {
	case raw := <-conn.out:
		var msg domain.BusMessage
		if err := json.Unmarshal(raw, &msg); err != nil || msg.Type != "registered" {
			t.Fatalf("ack %s", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("no ack")
	}
	if hub.Count() != 1 || len(hub.Tools(ctx)) != 1 {
		t.Fatalf("count %d tools %d", hub.Count(), len(hub.Tools(ctx)))
	}
	go func() {
		raw := <-conn.out
		var msg domain.BusMessage
		_ = json.Unmarshal(raw, &msg)
		body, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: "ок"})
		conn.in <- body
	}()
	got, err := hub.Invoke(ctx, domain.ToolCall{Name: "web_search", Arguments: `{"query":"x"}`})
	if err != nil || got != "ок" {
		t.Fatalf("invoke %q %v", got, err)
	}
	close(conn.in)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("accept did not return")
	}
	if hub.Count() != 0 || len(hub.Tools(context.Background())) != 0 {
		t.Fatalf("still registered: %d", hub.Count())
	}
}

func TestHubSoundLevel(t *testing.T) {
	hub := NewHub()
	conn := newChanConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = hub.Accept(ctx, conn) }()
	reg, _ := json.Marshal(domain.BusMessage{Type: "register", Name: "musicyoutube", Sound: true})
	conn.in <- reg
	select {
	case <-conn.out:
	case <-time.After(time.Second):
		t.Fatal("no ack")
	}
	go func() {
		for raw := range conn.out {
			var msg domain.BusMessage
			_ = json.Unmarshal(raw, &msg)
			content := `{"percent":80,"active":true}`
			if msg.Arguments != "" {
				content = "ok"
			}
			body, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: content})
			conn.in <- body
		}
	}()
	levels := hub.Levels(ctx)
	if len(levels) != 1 || levels[0].Name != "musicyoutube" || levels[0].Percent != 80 || !levels[0].Active {
		t.Fatalf("levels %+v", levels)
	}
	if err := hub.SetLevel(ctx, "musicyoutube", 56); err != nil {
		t.Fatal(err)
	}
}
