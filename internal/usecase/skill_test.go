package usecase

import (
	"context"
	"errors"
	"testing"

	"yascore/internal/domain"
)

type memBus struct {
	tools  []domain.Tool
	hints  []string
	last   domain.ToolCall
	body   string
	err    error
	pauses int
}

func (m *memBus) Tools(context.Context) []domain.Tool { return m.tools }
func (m *memBus) Hints(context.Context) []string      { return m.hints }
func (m *memBus) Count() int                          { return len(m.tools) }
func (m *memBus) Invoke(_ context.Context, call domain.ToolCall) (string, error) {
	m.last = call
	if m.err != nil {
		return "", m.err
	}
	return m.body, nil
}

func (m *memBus) PauseAll(context.Context) error {
	m.pauses++
	return nil
}

func TestWantsTools(t *testing.T) {
	name, _ := exampleAssistant(t)
	if !wantsTools(name+", найди курс", []string{"найди"}) {
		t.Fatal("expected hint match")
	}
	if wantsTools("привет", []string{"найди"}) {
		t.Fatal("unexpected hint match")
	}
}

func TestToolLoopKeepsChoiceString(t *testing.T) {
	name, words := exampleAssistant(t)
	bus := &memBus{
		tools: []domain.Tool{{Name: "web_search", Description: "поиск"}},
		hints: []string{"найди"},
		body:  "доллар 90",
	}
	llm := &fakeLLM{script: []domain.Completion{
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "web_search", Arguments: `{"query":"курс"}`}}},
		{Text: "Курс около 90.", Model: "fake"},
	}}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	reply, err := u.HandleUtterance(context.Background(), name+", найди курс доллара")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Курс около 90." {
		t.Fatalf("reply %q", reply.Text)
	}
	if bus.last.Name != "web_search" {
		t.Fatalf("invoke %+v", bus.last)
	}
	if len(llm.seen) != 2 {
		t.Fatalf("rounds %d", len(llm.seen))
	}
	if llm.seen[0].ToolChoice != "required" {
		t.Fatalf("first choice %q", llm.seen[0].ToolChoice)
	}
	if llm.seen[1].ToolChoice != "auto" {
		t.Fatalf("second choice %q", llm.seen[1].ToolChoice)
	}
	if llm.seen[1].Messages[2].Role != "tool" || llm.seen[1].Messages[2].Content != "доллар 90" {
		t.Fatalf("tool message %+v", llm.seen[1].Messages)
	}
}

func TestWeatherSentenceSkipsSecondModelCall(t *testing.T) {
	name, words := exampleAssistant(t)
	sentence := "В городе Москва сейчас пять и ясно"
	bus := &memBus{
		tools: []domain.Tool{{Name: "weather", Description: "погода"}},
		hints: []string{"погод"},
		body:  sentence + "\nasks_today: 2",
	}
	llm := &fakeLLM{script: []domain.Completion{
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "weather", Arguments: `{"city":"Москва"}`}}, Model: "fake"},
		{Text: "это не должно прозвучать", Model: "fake"},
	}}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	reply, err := u.HandleUtterance(context.Background(), name+", какая погода")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != sentence {
		t.Fatalf("reply %q", reply.Text)
	}
	if len(llm.seen) != 1 {
		t.Fatalf("rounds %d", len(llm.seen))
	}
}

func TestPlayerResultSkipsSecondModelCall(t *testing.T) {
	bus := &memBus{
		tools: []domain.Tool{{Name: "play_music"}},
		hints: []string{"музык"},
		body:  "играет Get Lucky — Daft Punk",
	}
	llm := &fakeLLM{script: []domain.Completion{
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "play_music", Arguments: `{"query":"Get Lucky","kind":"track"}`}}, Model: "fake"},
		{Text: "это не должно прозвучать", Model: "fake"},
	}}
	u := New(Deps{LLM: llm, Skills: bus, Prompt: "p"})
	reply, err := u.HandleUtterance(context.Background(), "включи Get Lucky")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "играет Get Lucky — Daft Punk" {
		t.Fatalf("reply %q", reply.Text)
	}
	if len(llm.seen) != 1 {
		t.Fatalf("rounds %d", len(llm.seen))
	}
}

func TestToolErrorIsFedBack(t *testing.T) {
	bus := &memBus{
		tools: []domain.Tool{{Name: "web_search"}},
		hints: []string{"найди"},
		err:   errors.New("поиск не удался"),
	}
	llm := &fakeLLM{script: []domain.Completion{
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "web_search", Arguments: "{}"}}},
		{Text: "Поиск не ответил.", Model: "fake"},
	}}
	u := New(Deps{LLM: llm, Skills: bus, Prompt: "p"})
	reply, err := u.HandleUtterance(context.Background(), "найди погоду")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Поиск не ответил." {
		t.Fatalf("reply %q", reply.Text)
	}
	if llm.seen[1].Messages[2].Content != "поиск не удался" {
		t.Fatalf("content %q", llm.seen[1].Messages[2].Content)
	}
}

func TestDisconnectedBusDropsTools(t *testing.T) {
	bus := &memBus{tools: []domain.Tool{{Name: "web_search"}}, hints: []string{"найди"}}
	llm := &fakeLLM{text: "без инструментов"}
	u := New(Deps{LLM: llm, Skills: bus, Prompt: "p"})
	bus.tools = nil
	bus.hints = nil
	reply, err := u.HandleUtterance(context.Background(), "найди погоду")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "без инструментов" {
		t.Fatal(reply.Text)
	}
	if len(llm.seen[0].Tools) != 0 {
		t.Fatalf("tools still sent: %+v", llm.seen[0].Tools)
	}
}
