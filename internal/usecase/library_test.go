package usecase

import (
	"context"
	"strings"
	"testing"

	"yascore/internal/domain"
)

func TestWakePlayerCommandsSkipTheModel(t *testing.T) {
	name, words := exampleAssistant(t)
	commands := []struct {
		phrase, tool, arg, memory string
	}{
		{name + ", дальше", "music_control", "next", "Следующий трек."},
		{name + ", вперед", "music_control", "next", "Следующий трек."},
		{name + ", вперёд", "music_control", "next", "Следующий трек."},
		{name + ", назад", "music_control", "previous", "Предыдущий трек."},
		{name + ", " + name + ", предыдущий", "music_control", "previous", "Предыдущий трек."},
		{name + ", предыдущий трек", "music_control", "previous", "Предыдущий трек."},
		{name + ", останови музыку", "music_control", "stop", "Остановила музыку."},
	}
	for _, tc := range commands {
		t.Run(tc.phrase, func(t *testing.T) {
			bus := &memBus{body: domain.SilentReply}
			llm := &fakeLLM{text: "не должна говорить"}
			u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
			reply, err := u.HandleUtterance(context.Background(), tc.phrase)
			if err != nil {
				t.Fatal(err)
			}
			if !reply.Quiet || reply.Text != tc.memory {
				t.Fatalf("reply %+v", reply)
			}
			if bus.last.Name != tc.tool || !strings.Contains(bus.last.Arguments, tc.arg) {
				t.Fatalf("call %+v", bus.last)
			}
			if len(llm.seen) != 0 {
				t.Fatalf("model rounds %d", len(llm.seen))
			}
		})
	}
}

func TestBareMusicSkipsTheModel(t *testing.T) {
	name, words := exampleAssistant(t)
	for _, phrase := range []string{"музыка", name + ", музыку", "включай", "включи музыку", "поставь музыку"} {
		t.Run(phrase, func(t *testing.T) {
			bus := &memBus{body: domain.SilentReply}
			llm := &fakeLLM{text: "уже играет"}
			u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
			reply, err := u.HandleUtterance(context.Background(), phrase)
			if err != nil {
				t.Fatal(err)
			}
			if !reply.Quiet || reply.Text != "Включила музыку." {
				t.Fatalf("reply %+v", reply)
			}
			if bus.last.Name != "play_music" || !strings.Contains(bus.last.Arguments, `"kind":"any"`) {
				t.Fatalf("call %+v", bus.last)
			}
			if len(llm.seen) != 0 {
				t.Fatalf("model rounds %d", len(llm.seen))
			}
		})
	}
}

func TestLikeCommandsAreSpoken(t *testing.T) {
	name, words := exampleAssistant(t)
	commands := []struct {
		phrase, tool, arg, spoken string
	}{
		{"лайк", "music_control", "like", domain.LikedReply},
		{"нравится", "music_control", "like", domain.LikedReply},
		{"like", "music_control", "like", domain.LikedReply},
		{"убери лайк", "music_control", "unlike", domain.UnlikedReply},
		{"не нравится", "music_control", "unlike", domain.UnlikedReply},
		{"unlike", "music_control", "unlike", domain.UnlikedReply},
	}
	for _, tc := range commands {
		t.Run(tc.phrase, func(t *testing.T) {
			bus := &memBus{
				tools: []domain.Tool{{Name: "play_music"}, {Name: "music_control"}},
				hints: []string{"лайк", "нравится", "включи"},
				body:  tc.spoken,
			}
			llm := &fakeLLM{text: "назвала трек вслух"}
			u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
			if _, err := u.HandleUtterance(context.Background(), name+", расскажи про погоду"); err != nil {
				t.Fatal(err)
			}
			reply, err := u.HandleUtterance(context.Background(), name+", "+tc.phrase)
			if err != nil {
				t.Fatal(err)
			}
			if reply.Quiet || reply.Text != tc.spoken {
				t.Fatalf("reply %+v", reply)
			}
			if bus.last.Name != tc.tool || !strings.Contains(bus.last.Arguments, tc.arg) {
				t.Fatalf("call %+v", bus.last)
			}
			if len(llm.seen) != 1 {
				t.Fatalf("model rounds %d", len(llm.seen))
			}
		})
	}
}

func TestPlayLibraryCommandsStayQuiet(t *testing.T) {
	name, words := exampleAssistant(t)
	commands := []struct {
		phrase, tool, arg, memory string
	}{
		{"включи лайки", "play_music", "likes", "Включила из лайков."},
		{"включи что-нибудь из лайков", "play_music", "likes", "Включила из лайков."},
		{"что-нибудь такое же", "play_music", "similar", "Похожее."},
	}
	for _, tc := range commands {
		t.Run(tc.phrase, func(t *testing.T) {
			bus := &memBus{
				tools: []domain.Tool{{Name: "play_music"}, {Name: "music_control"}},
				hints: []string{"лайк", "нравится", "включи"},
				body:  "играет Get Lucky — Daft Punk",
			}
			llm := &fakeLLM{text: "назвала трек вслух"}
			u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
			if _, err := u.HandleUtterance(context.Background(), name+", расскажи про погоду"); err != nil {
				t.Fatal(err)
			}
			reply, err := u.HandleUtterance(context.Background(), name+", "+tc.phrase)
			if err != nil {
				t.Fatal(err)
			}
			if !reply.Quiet || reply.Text != tc.memory {
				t.Fatalf("reply %+v", reply)
			}
			if strings.Contains(reply.Text, "Get Lucky") || strings.Contains(reply.Text, "Daft Punk") {
				t.Fatalf("track announced: %+v", reply)
			}
			if bus.last.Name != tc.tool || !strings.Contains(bus.last.Arguments, tc.arg) {
				t.Fatalf("call %+v", bus.last)
			}
			if len(llm.seen) != 1 {
				t.Fatalf("model rounds %d", len(llm.seen))
			}
		})
	}
}

func TestEmptyLikesAreSpokenOnce(t *testing.T) {
	name, words := exampleAssistant(t)
	phrases := []string{"включи лайки", "включи что-нибудь из лайков", "что-нибудь такое же"}
	for _, phrase := range phrases {
		t.Run(phrase, func(t *testing.T) {
			bus := &memBus{
				tools: []domain.Tool{{Name: "play_music"}},
				hints: []string{"лайк", "включи", "такое"},
				body:  "Пока ничего не нравится.",
			}
			llm := &fakeLLM{script: []domain.Completion{{Text: "вот случайный трек", Model: "fake"}}}
			u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
			reply, err := u.HandleUtterance(context.Background(), name+", "+phrase)
			if err != nil {
				t.Fatal(err)
			}
			if reply.Quiet || reply.Text != "Пока ничего не нравится." {
				t.Fatalf("reply %+v", reply)
			}
			if len(llm.seen) != 0 {
				t.Fatalf("model invented a follow-up: %+v", llm.seen)
			}
		})
	}
}

func TestEmptyLikesFromTheModelAreNotRewritten(t *testing.T) {
	bus := &memBus{
		tools: []domain.Tool{{Name: "play_music"}},
		hints: []string{"лайк"},
		body:  "Пока ничего не нравится.",
	}
	llm := &fakeLLM{script: []domain.Completion{
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "play_music", Arguments: `{"query":"","kind":"likes"}`}}, Model: "fake"},
		{Text: "включаю Get Lucky", Model: "fake"},
	}}
	u := New(Deps{LLM: llm, Skills: bus, Prompt: "p"})
	reply, err := u.HandleUtterance(context.Background(), "покажи лайки")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Quiet || reply.Text != "Пока ничего не нравится." {
		t.Fatalf("reply %+v", reply)
	}
	if len(llm.seen) != 1 {
		t.Fatalf("rounds %d", len(llm.seen))
	}
}
