package usecase

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yascore/internal/domain"
	"yascore/internal/repo"
)

func TestCommandLogToolAndDuck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.jsonl")
	clog, err := repo.NewCommandLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })

	bus := &memBus{
		tools: []domain.Tool{{Name: "weather", Description: "погода"}},
		hints: []string{"погод"},
		body:  "В городе Москва сейчас ясно.",
	}
	llm := &fakeLLM{script: []domain.Completion{
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "weather", Arguments: `{"city":"Москва"}`}}, Model: "fake"},
	}}
	name, words := exampleAssistant(t)
	u := New(Deps{
		LLM: llm, Wake: fakeWake{words: words}, Skills: bus,
		Commands: clog, Prompt: "p",
	})
	board := &memBoard{levels: []domain.SoundLevel{{Name: "musicyoutube", Percent: 80, Active: true}}}
	d := NewDucker(board)

	ctx, scope := OpenCommand(context.Background(), "http", clog)
	gen := d.Begin(ctx)
	if _, err := u.HandleUtterance(ctx, name+", какая погода"); err != nil {
		t.Fatal(err)
	}
	d.End(ctx, gen)
	scope.Finish()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("lines %d: %s", len(lines), body)
	}
	if !strings.Contains(lines[0], `"name":"weather"`) || !strings.Contains(lines[0], `"name":"duck"`) || !strings.Contains(lines[0], `"ms"`) {
		t.Fatalf("tool and duck with ms missing: %s", lines[0])
	}
	var rec domain.Command
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	var sawTool, sawDuck bool
	for _, action := range rec.Actions {
		switch action.Name {
		case "weather":
			sawTool = true
			if action.Start == "" || action.End == "" {
				t.Fatalf("tool clock %+v", action)
			}
		case "duck":
			sawDuck = true
			if action.Start == "" || action.End == "" {
				t.Fatalf("duck clock %+v", action)
			}
		}
	}
	if !sawTool || !sawDuck {
		t.Fatalf("actions %+v", rec.Actions)
	}
	if rec.Source != "http" || rec.User != "какая погода" {
		t.Fatalf("record %+v", rec)
	}
}
