package repo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"yascore/internal/domain"
)

func TestOllamaSendsStringToolChoice(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"model":"qwen","choices":[{"message":{"content":"ок"}}]}`))
	}))
	defer srv.Close()
	client := NewOllama(srv.URL+"/v1", "qwen", time.Second)
	_, err := client.Complete(context.Background(), domain.CompleteRequest{
		System: "sys", Model: "qwen", ToolChoice: "required",
		Messages: []domain.Message{{Role: "user", Content: "привет"}},
		Tools:    []domain.Tool{{Name: "web_search", Description: "поиск"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	choice, ok := bodies[0]["tool_choice"].(string)
	if !ok || choice != "required" {
		t.Fatalf("tool_choice %#v", bodies[0]["tool_choice"])
	}
	_, err = client.Complete(context.Background(), domain.CompleteRequest{
		System: "sys", Messages: []domain.Message{{Role: "user", Content: "ещё"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := bodies[1]["tools"]; exists {
		t.Fatal("tools sent without tools")
	}
}

func TestOllamaParsesToolArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"qwen","choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","function":{"name":"web_search","arguments":{"query":"курс"}}}]}}]}`))
	}))
	defer srv.Close()
	client := NewOllama(srv.URL+"/v1", "qwen", time.Second)
	got, err := client.Complete(context.Background(), domain.CompleteRequest{
		Messages: []domain.Message{{Role: "user", Content: "x"}},
		Tools:    []domain.Tool{{Name: "web_search"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "web_search" {
		t.Fatalf("%+v", got.ToolCalls)
	}
	if got.ToolCalls[0].Arguments != `{"query":"курс"}` {
		t.Fatalf("args %s", got.ToolCalls[0].Arguments)
	}
}

func TestOllama5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("down"))
	}))
	defer srv.Close()
	client := NewOllama(srv.URL+"/v1", "qwen", time.Second)
	_, err := client.Complete(context.Background(), domain.CompleteRequest{
		Messages: []domain.Message{{Role: "user", Content: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "llm_5xx") {
		t.Fatal(err)
	}
}
