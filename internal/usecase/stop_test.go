package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode"

	"yascore/internal/domain"
)

func TestStopAllClearsMemory(t *testing.T) {
	name, words := exampleAssistant(t)
	bus := &memBus{}
	llm := &fakeLLM{text: "ясно"}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	if _, err := u.HandleUtterance(context.Background(), name+", расскажи про погоду"); err != nil {
		t.Fatal(err)
	}
	reply, err := u.StopAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Quiet || reply.Text != "Остановилась." {
		t.Fatalf("reply %+v", reply)
	}
	if bus.pauses == 0 {
		t.Fatal("sound skills were not paused")
	}
	if memoryHas(u, "погоду", "ясно") {
		t.Fatalf("previous turn still stored: %+v", u.mem)
	}
}

func TestStopAllWithoutSkills(t *testing.T) {
	u := New(Deps{LLM: &fakeLLM{text: "x"}, Wake: fakeWake{}})
	if _, err := u.HandleUtterance(context.Background(), "привет"); err != nil {
		t.Fatal(err)
	}
	reply, err := u.StopAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Quiet || reply.Text != "Остановилась." {
		t.Fatalf("reply %+v", reply)
	}
	if memoryHas(u, "привет") {
		t.Fatalf("memory %+v", u.mem)
	}
}

func TestBareStopSkipsTheModelAndForgetsDialogue(t *testing.T) {
	name, words := exampleAssistant(t)
	phrases := []string{
		name + ", стоп",
		"стоп!",
		"хватит",
		"остановись",
		"замолчи",
		"тише",
		"Помолчи.",
		name + ", " + name + ", СТОП!",
	}
	for _, phrase := range phrases {
		t.Run(phrase, func(t *testing.T) {
			bus := &memBus{}
			llm := &fakeLLM{text: "ясно"}
			u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
			if _, err := u.HandleUtterance(context.Background(), name+", расскажи про погоду"); err != nil {
				t.Fatal(err)
			}
			reply, err := u.HandleUtterance(context.Background(), phrase)
			if err != nil {
				t.Fatal(err)
			}
			if !reply.Quiet || reply.Text != "Остановилась." {
				t.Fatalf("reply %+v", reply)
			}
			if len(llm.seen) != 1 {
				t.Fatalf("model calls %d", len(llm.seen))
			}
			if bus.last.Name == "music_control" {
				t.Fatalf("music_control %+v", bus.last)
			}
			if _, err := u.HandleUtterance(context.Background(), "кто поёт?"); err != nil {
				t.Fatal(err)
			}
			last := llm.seen[len(llm.seen)-1]
			if memoryHasMessages(last.Messages, "погоду", "ясно") {
				t.Fatalf("old turns leaked into %+v", last.Messages)
			}
			if !sameWords(last.Messages[len(last.Messages)-1].Content, "кто поёт?") {
				t.Fatalf("question %+v", last.Messages)
			}
		})
	}
}

func TestEchoOfSpokenIsDropped(t *testing.T) {
	llm := &fakeLLM{script: []domain.Completion{
		{Text: "Сегодня пятница.", Model: "fake"},
		{Text: "Дафт Панк", Model: "fake"},
	}}
	name, words := exampleAssistant(t)
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Prompt: "p"})
	reply, err := u.HandleUtterance(context.Background(), name+", какой день")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Quiet || reply.Text != "Сегодня пятница." {
		t.Fatalf("spoken %+v", reply)
	}
	u.HoldSpeech(2 * time.Second)
	_, err = u.HandleUtterance(context.Background(), name+", Сегодня пятница.")
	if !errors.Is(err, domain.ErrEmpty) {
		t.Fatalf("echo err %v", err)
	}
	if len(llm.seen) != 1 {
		t.Fatalf("echo called the model: %+v", llm.seen)
	}
	reply, err = u.HandleUtterance(context.Background(), name+", Сегодня пятница. кто поёт?")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Дафт Панк" {
		t.Fatalf("reply %+v", reply)
	}
	last := llm.seen[len(llm.seen)-1]
	user := last.Messages[len(last.Messages)-1].Content
	if !sameWords(user, "кто поёт?") {
		t.Fatalf("user line %q", user)
	}
	u.clearSpeech()
	reply, err = u.HandleUtterance(context.Background(), "Сегодня пятница.")
	if err != nil {
		t.Fatalf("after speech cleared: %v", err)
	}
	if reply.Text == "" {
		t.Fatal("cleared speech still dropped the sentence")
	}
}

func TestStopInsideEchoStopsAll(t *testing.T) {
	name, words := exampleAssistant(t)
	bus := &memBus{}
	llm := &fakeLLM{text: "Сегодня пятница."}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	if _, err := u.HandleUtterance(context.Background(), name+", какой день"); err != nil {
		t.Fatal(err)
	}
	u.HoldSpeech(2 * time.Second)
	reply, err := u.HandleUtterance(context.Background(), "Сегодня пятница, стоп")
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Quiet || reply.Text != "Остановилась." {
		t.Fatalf("reply %+v", reply)
	}
	if len(llm.seen) != 1 {
		t.Fatalf("model calls %d", len(llm.seen))
	}
	if bus.pauses == 0 && bus.last.Name == "music_control" {
		t.Fatalf("echoed stop only called music_control: %+v", bus.last)
	}
	if _, err := u.HandleUtterance(context.Background(), "кто поёт?"); err != nil {
		t.Fatal(err)
	}
	last := llm.seen[len(llm.seen)-1]
	if memoryHasMessages(last.Messages, "какой день", "Сегодня пятница.") {
		t.Fatalf("old turns leaked: %+v", last.Messages)
	}
}

func TestMusicStopDoesNotClearDialogue(t *testing.T) {
	name, words := exampleAssistant(t)
	bus := &memBus{body: domain.SilentReply}
	llm := &fakeLLM{text: "ясно"}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	if _, err := u.HandleUtterance(context.Background(), name+", расскажи про погоду"); err != nil {
		t.Fatal(err)
	}
	reply, err := u.HandleUtterance(context.Background(), "останови музыку")
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Quiet || reply.Text != "Остановила музыку." {
		t.Fatalf("reply %+v", reply)
	}
	if bus.last.Name != "music_control" || !strings.Contains(bus.last.Arguments, "stop") {
		t.Fatalf("call %+v", bus.last)
	}
	if len(llm.seen) != 1 {
		t.Fatalf("model calls %d", len(llm.seen))
	}
	if _, err := u.HandleUtterance(context.Background(), "кто поёт?"); err != nil {
		t.Fatal(err)
	}
	last := llm.seen[len(llm.seen)-1]
	if !messagesContain(last.Messages, "расскажи про погоду") || !messagesContain(last.Messages, "ясно") {
		t.Fatalf("dialogue was cleared: %+v", last.Messages)
	}
}

func TestChatDropsSilentAndEchoMarkers(t *testing.T) {
	_, words := exampleAssistant(t)
	llm := &fakeLLM{text: "да"}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Prompt: "p"})
	_, err := u.HandleChat(context.Background(), []domain.Message{
		{Role: "user", Content: "включи Get Lucky"},
		{Role: "assistant", Content: domain.SilentReply},
		{Role: "assistant", Content: domain.EchoReply},
	}, "кто поёт?")
	if err != nil {
		t.Fatal(err)
	}
	got := llm.seen[0].Messages
	if len(got) != 2 || got[0].Content != "включи Get Lucky" || got[1].Content != "кто поёт?" {
		t.Fatalf("messages %+v", got)
	}
	if memoryHasMessages(got, domain.SilentReply, domain.EchoReply) {
		t.Fatalf("markers leaked: %+v", got)
	}
}

func TestMusicCommandsDoNotStopAll(t *testing.T) {
	name, words := exampleAssistant(t)
	commands := []struct {
		phrase, action, memory string
	}{
		{"пауза", "pause", "Пауза."},
		{"поставь на паузу", "pause", "Пауза."},
		{"дальше", "next", "Следующий трек."},
		{"вперед", "next", "Следующий трек."},
		{"вперёд", "next", "Следующий трек."},
		{"следующая песня", "next", "Следующий трек."},
		{"назад", "previous", "Предыдущий трек."},
		{"предыдущий", "previous", "Предыдущий трек."},
		{"предыдущий трек", "previous", "Предыдущий трек."},
		{"предыдущая песня", "previous", "Предыдущий трек."},
		{"продолжи", "resume", "Продолжила музыку."},
		{"продолжи музыку", "resume", "Продолжила музыку."},
		{"стоп музыку", "stop", "Остановила музыку."},
		{"выключи ютуб", "stop", "Остановила музыку."},
	}
	for _, tc := range commands {
		t.Run(tc.phrase, func(t *testing.T) {
			bus := &memBus{body: domain.SilentReply}
			llm := &fakeLLM{text: "ясно"}
			u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
			if _, err := u.HandleUtterance(context.Background(), name+", расскажи про погоду"); err != nil {
				t.Fatal(err)
			}
			reply, err := u.HandleUtterance(context.Background(), tc.phrase)
			if err != nil {
				t.Fatal(err)
			}
			if !reply.Quiet || reply.Text != tc.memory {
				t.Fatalf("reply %+v", reply)
			}
			if bus.last.Name != "music_control" || !strings.Contains(bus.last.Arguments, tc.action) {
				t.Fatalf("call %+v", bus.last)
			}
			if len(llm.seen) != 1 {
				t.Fatalf("model calls %d", len(llm.seen))
			}
			if _, err := u.HandleUtterance(context.Background(), "кто поёт?"); err != nil {
				t.Fatal(err)
			}
			last := llm.seen[len(llm.seen)-1]
			if !messagesContain(last.Messages, "расскажи про погоду") || !messagesContain(last.Messages, "ясно") {
				t.Fatalf("dialogue was cleared: %+v", last.Messages)
			}
		})
	}
}

func TestQuietPlayIsRememberedAndIsNotSpoken(t *testing.T) {
	_, words := exampleAssistant(t)
	bus := &memBus{
		tools: []domain.Tool{{Name: "play_music"}},
		hints: []string{"включи"},
		body:  "играет Get Lucky — Daft Punk",
	}
	llm := &fakeLLM{script: []domain.Completion{
		{Text: "Сегодня пятница.", Model: "fake"},
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "play_music", Arguments: `{"query":"Get Lucky","kind":"track"}`}}, Model: "fake"},
		{Text: "Дафт Панк", Model: "fake"},
		{Text: "всё ещё Дафт Панк", Model: "fake"},
	}}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	if _, err := u.HandleUtterance(context.Background(), "какой день"); err != nil {
		t.Fatal(err)
	}
	reply, err := u.HandleUtterance(context.Background(), "включи Get Lucky")
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Quiet || !strings.HasPrefix(reply.Text, "играет ") {
		t.Fatalf("play reply %+v", reply)
	}
	u.HoldSpeech(2 * time.Second)
	_, err = u.HandleUtterance(context.Background(), "Сегодня пятница.")
	if !errors.Is(err, domain.ErrEmpty) {
		t.Fatalf("spoken echo err %v", err)
	}
	if len(llm.seen) != 2 {
		t.Fatalf("echo after quiet play called the model, rounds %d", len(llm.seen))
	}
	reply, err = u.HandleUtterance(context.Background(), "играет Get Lucky — Daft Punk")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Дафт Панк" {
		t.Fatalf("quiet line was treated as spoken echo: %+v", reply)
	}
	user := llm.seen[len(llm.seen)-1].Messages
	if !messagesContain(user, "играет Get Lucky — Daft Punk") {
		t.Fatalf("music line stripped as echo: %+v", user)
	}
	if _, err := u.HandleUtterance(context.Background(), "кто поёт?"); err != nil {
		t.Fatal(err)
	}
	last := llm.seen[len(llm.seen)-1]
	if !messagesContain(last.Messages, "играет Get Lucky — Daft Punk") || !messagesContain(last.Messages, "кто поёт") {
		t.Fatalf("history %+v", last.Messages)
	}
}

func memoryHas(u *Utterance, parts ...string) bool {
	u.memMu.Lock()
	defer u.memMu.Unlock()
	return memoryHasMessages(u.mem, parts...)
}

func memoryHasMessages(msgs []domain.Message, parts ...string) bool {
	for _, m := range msgs {
		for _, part := range parts {
			if strings.Contains(m.Content, part) {
				return true
			}
		}
	}
	return false
}

func messagesContain(msgs []domain.Message, part string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, part) {
			return true
		}
	}
	return false
}

func sameWords(got, want string) bool {
	gw, ww := normWords(got), normWords(want)
	if len(gw) != len(ww) {
		return false
	}
	for i := range gw {
		if gw[i] != ww[i] {
			return false
		}
	}
	return true
}

func normWords(s string) []string {
	var out []string
	for _, field := range strings.Fields(strings.ToLower(s)) {
		var b strings.Builder
		for _, r := range field {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		if b.Len() > 0 {
			out = append(out, b.String())
		}
	}
	return out
}
