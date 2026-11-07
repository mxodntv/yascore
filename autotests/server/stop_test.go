//go:build integration

package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"yascore/autotests/stats"
	"yascore/internal/domain"
	"yascore/internal/repo"
	"yascore/internal/server"
	"yascore/internal/usecase"
)

func TestStopAllPausesSoundSkill(t *testing.T) {
	name, words := exampleAssistant(t)
	model := &callLLM{}
	hub := repo.NewHub()
	u := usecase.New(usecase.Deps{
		LLM: model, Wake: configuredWake{words: words}, Skills: hub, Prompt: "p",
	})
	edge := server.New(u, hub, server.Info{Assistant: name}, t.TempDir(), "http://127.0.0.1:8091")
	ts := httptest.NewServer(edge.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/v1/skills", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	reg, _ := json.Marshal(domain.BusMessage{Type: "register", Name: "youtubemusic", Sound: true})
	if err := conn.Write(ctx, websocket.MessageText, reg); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var pauses int
	go func() {
		for ctx.Err() == nil {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg domain.BusMessage
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			if msg.Type == "pause" {
				mu.Lock()
				pauses++
				mu.Unlock()
			}
			if msg.ID == "" {
				continue
			}
			out, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: "ok"})
			_ = conn.Write(ctx, websocket.MessageText, out)
		}
	}()

	start := time.Now()
	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": name + ", стоп"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(string(payload)))
	elapsed := time.Since(start).Milliseconds()
	rec := stats.Record{Suite: "stopall", N: 1, Pass: 1, Stages: map[string]stats.Stage{
		"e2e": stats.FromSamples([]int64{elapsed}),
	}}
	if err != nil {
		rec.Pass, rec.Fail = 0, 1
		rec.Errors = []stats.ErrCount{{Code: "stop_post", Count: 1}}
		_ = stats.Publish(rec)
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var body struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(raw, &body)
	content := ""
	if len(body.Choices) == 1 {
		content = body.Choices[0].Message.Content
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	var got int
	for time.Now().Before(deadline) {
		mu.Lock()
		got = pauses
		mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp.StatusCode != http.StatusOK || content != domain.SilentReply || got == 0 || model.calls != 0 {
		rec.Pass, rec.Fail = 0, 1
		rec.Errors = []stats.ErrCount{{Code: "stop_pause", Count: 1}}
		t.Errorf("status %d content %q pauses %d llm %d body %s", resp.StatusCode, content, got, model.calls, raw)
	}
	if pubErr := stats.Publish(rec); pubErr != nil {
		t.Fatal(pubErr)
	}
}

type callLLM struct{ calls int }

func (c *callLLM) Complete(context.Context, domain.CompleteRequest) (domain.Completion, error) {
	c.calls++
	return domain.Completion{Text: "не молчи", Model: "fake"}, nil
}

type configuredWake struct{ words []string }

func (w configuredWake) Strip(text string) string {
	return domain.Strip(text, w.words)
}
