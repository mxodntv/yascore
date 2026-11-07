package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"yascore/internal/domain"
	"yascore/internal/server"
	"yascore/internal/usecase"
)

type llm struct{ text string }

func (l llm) Complete(context.Context, domain.CompleteRequest) (domain.Completion, error) {
	return domain.Completion{Text: l.text, Model: "fake"}, nil
}

type wake struct{}

func (wake) Strip(text string) string { return text }

func TestHealthAndUtterance(t *testing.T) {
	name, _ := exampleAssistant(t)
	u := usecase.New(usecase.Deps{LLM: llm{text: "привет"}, Wake: wake{}, Prompt: "p", Model: "fake"})
	srv := server.New(u, nil, server.Info{Assistant: name, LLM: "fake", STT: "faster-whisper", TTS: "silero"}, t.TempDir(), "http://127.0.0.1:8091")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health["assistant"] != name || health["status"] != "ok" {
		t.Fatalf("health %#v", health)
	}

	resp, err = http.Post(ts.URL+"/v1/utterance", "application/json", bytes.NewBufferString(`{"text":"привет"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["text"] != "привет" {
		t.Fatalf("body %#v", body)
	}
}

func TestChatCompletionsReturnsFinalText(t *testing.T) {
	u := usecase.New(usecase.Deps{LLM: llm{text: "да"}, Wake: wake{}, Prompt: "p", Model: "fake"})
	srv := server.New(u, nil, server.Info{LLM: "fake"}, t.TempDir(), "http://127.0.0.1:8091")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{"messages":[{"role":"system","content":"x"},{"role":"user","content":"скажи да"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Choices) != 1 || body.Choices[0].Message.Content != "да" {
		t.Fatalf("body %+v", body)
	}
}

type recLLM struct {
	text string
	seen []domain.CompleteRequest
}

func (r *recLLM) Complete(_ context.Context, req domain.CompleteRequest) (domain.Completion, error) {
	r.seen = append(r.seen, req)
	return domain.Completion{Text: r.text, Model: "fake"}, nil
}

func TestChatCompletionsKeepsDialogue(t *testing.T) {
	model := &recLLM{text: "Дафт Панк"}
	u := usecase.New(usecase.Deps{LLM: model, Wake: wake{}, Prompt: "p", Model: "fake"})
	srv := server.New(u, nil, server.Info{LLM: "fake"}, t.TempDir(), "http://127.0.0.1:8091")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	body := `{"messages":[{"role":"system","content":"x"},{"role":"user","content":"включи Get Lucky"},{"role":"assistant","content":"играет Get Lucky"},{"role":"user","content":"кто поёт?"}]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if len(model.seen) != 1 || len(model.seen[0].Messages) != 3 || model.seen[0].Messages[2].Content != "кто поёт?" {
		t.Fatalf("messages %+v", model.seen)
	}
}

type countLLM struct {
	text  string
	calls int
}

func (c *countLLM) Complete(context.Context, domain.CompleteRequest) (domain.Completion, error) {
	c.calls++
	return domain.Completion{Text: c.text, Model: "fake"}, nil
}

type stripWake struct{ words []string }

func (w stripWake) Strip(text string) string {
	return domain.Strip(text, w.words)
}

func TestChatCompletionsStopIsSilent(t *testing.T) {
	name, words := exampleAssistant(t)
	model := &countLLM{text: "сорок два"}
	u := usecase.New(usecase.Deps{LLM: model, Wake: stripWake{words: words}, Prompt: "p", Model: "fake"})
	srv := server.New(u, nil, server.Info{LLM: "fake"}, t.TempDir(), "http://127.0.0.1:8091")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload, err := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": name + ", стоп"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := chatContent(t, resp); got != domain.SilentReply {
		t.Fatalf("content %q", got)
	}
	if model.calls != 0 {
		t.Fatalf("model was asked to speak, calls %d", model.calls)
	}

	resp, err = http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewBufferString(`{"messages":[{"role":"user","content":"сколько будет дважды два"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := chatContent(t, resp); got != "сорок два" || got == domain.SilentReply {
		t.Fatalf("content %q", got)
	}
	if model.calls != 1 {
		t.Fatalf("calls %d", model.calls)
	}
}

func chatContent(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Choices) != 1 {
		t.Fatalf("choices %+v", body.Choices)
	}
	return body.Choices[0].Message.Content
}

func TestUtteranceEmpty(t *testing.T) {
	u := usecase.New(usecase.Deps{LLM: llm{text: "x"}, Wake: wake{}})
	srv := server.New(u, nil, server.Info{}, t.TempDir(), "http://127.0.0.1")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/v1/utterance", "application/json", bytes.NewBufferString(`{"text":"  "}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
