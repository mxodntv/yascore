package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"yascore/internal/domain"
	"yascore/internal/repo"
	"yascore/internal/server"
	"yascore/internal/usecase"
)

type scriptLLM struct {
	step int
}

func (s *scriptLLM) Complete(_ context.Context, req domain.CompleteRequest) (domain.Completion, error) {
	s.step++
	if s.step == 1 {
		return domain.Completion{ToolCalls: []domain.ToolCall{{
			ID: "1", Name: "echo", Arguments: `{"q":"a"}`,
		}}}, nil
	}
	var tool string
	for _, m := range req.Messages {
		if m.Role == "tool" {
			tool = m.Content
		}
	}
	return domain.Completion{Text: "ответ " + tool, Model: "fake"}, nil
}

type passWake struct{}

func (passWake) Strip(text string) string { return text }

func TestSkillSocketRoundTrip(t *testing.T) {
	name, _ := exampleAssistant(t)
	hub := repo.NewHub()
	u := usecase.New(usecase.Deps{
		LLM: &scriptLLM{}, Wake: passWake{}, Skills: hub, Prompt: "p",
	})
	srv := server.New(u, hub, server.Info{Assistant: name, Skills: hub.Count}, t.TempDir(), "http://127.0.0.1")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/skills"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	reg, _ := json.Marshal(domain.BusMessage{
		Type: "register", Name: "echo-skill", Hints: []string{"эхо"},
		Tools: []domain.Tool{{Name: "echo", Description: "эхо"}},
	})
	if err := conn.Write(ctx, websocket.MessageText, reg); err != nil {
		t.Fatal(err)
	}
	_, ack, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ack), "registered") {
		t.Fatalf("ack %s", ack)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg domain.BusMessage
		_ = json.Unmarshal(raw, &msg)
		out, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: "pong"})
		_ = conn.Write(ctx, websocket.MessageText, out)
	}()

	resp, err := http.Post(ts.URL+"/v1/utterance", "application/json", strings.NewReader(`{"text":"эхо"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["text"] != "ответ pong" {
		t.Fatalf("body %#v", body)
	}
	<-done
}
