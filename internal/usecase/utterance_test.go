package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"yascore/config"
	"yascore/internal/domain"
	"yascore/internal/repo"
	"yascore/skills/mood"
)

type fakeWake struct{ words []string }

func (f fakeWake) Strip(text string) string { return domain.Strip(text, f.words) }
func (f fakeWake) Words() []string          { return append([]string(nil), f.words...) }

type fakeLLM struct {
	err    error
	text   string
	seen   []domain.CompleteRequest
	script []domain.Completion
	step   int
}

func (f *fakeLLM) Complete(_ context.Context, req domain.CompleteRequest) (domain.Completion, error) {
	f.seen = append(f.seen, req)
	if f.err != nil {
		return domain.Completion{}, f.err
	}
	if f.step < len(f.script) {
		out := f.script[f.step]
		f.step++
		return out, nil
	}
	return domain.Completion{Text: f.text, Model: "fake"}, nil
}

func TestHandleUtteranceStripsWakeAndSetsPrompt(t *testing.T) {
	name, words := exampleAssistant(t)
	prompt := "Ты " + name + "."
	llm := &fakeLLM{text: "свет включён"}
	u := New(Deps{
		LLM: llm, Wake: fakeWake{words: words},
		Prompt: prompt, Model: "qwen",
	})
	reply, err := u.HandleUtterance(context.Background(), name+", включи свет")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "свет включён" {
		t.Fatalf("reply %q", reply.Text)
	}
	if len(llm.seen) != 1 {
		t.Fatalf("calls %d", len(llm.seen))
	}
	if llm.seen[0].System != prompt {
		t.Fatalf("system %q", llm.seen[0].System)
	}
	if llm.seen[0].Messages[0].Content != "включи свет" {
		t.Fatalf("user %q", llm.seen[0].Messages[0].Content)
	}
	if len(llm.seen[0].Tools) != 0 || llm.seen[0].ToolChoice != "" {
		t.Fatalf("tools leaked: %+v", llm.seen[0])
	}
}

func TestHandleUtteranceEmpty(t *testing.T) {
	u := New(Deps{LLM: &fakeLLM{text: "x"}, Wake: fakeWake{}})
	_, err := u.HandleUtterance(context.Background(), "   ")
	if !errors.Is(err, domain.ErrEmpty) {
		t.Fatalf("err %v", err)
	}
}

func TestHandleUtteranceLLMError(t *testing.T) {
	u := New(Deps{LLM: &fakeLLM{err: errors.New("llm_5xx")}, Wake: fakeWake{}})
	_, err := u.HandleUtterance(context.Background(), "привет")
	if err == nil || !strings.Contains(err.Error(), "llm_5xx") {
		t.Fatalf("err %v", err)
	}
}

func TestHandleUtteranceBlankModelText(t *testing.T) {
	u := New(Deps{LLM: &fakeLLM{text: "  "}, Wake: fakeWake{}})
	reply, err := u.HandleUtterance(context.Background(), "привет")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Готово." {
		t.Fatalf("reply %q", reply.Text)
	}
}

type fakeTTS struct {
	got string
}

func (f *fakeTTS) Synthesize(_ context.Context, text, voice string) (domain.Audio, error) {
	f.got = text
	return domain.Audio{MIME: "audio/wav", Bytes: []byte("RIFF"), Voice: voice}, nil
}

func TestDialogueRemembersAndExpires(t *testing.T) {
	name, words := exampleAssistant(t)
	llm := &fakeLLM{text: "ок"}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Prompt: "p"})
	if _, err := u.HandleUtterance(context.Background(), name+", включи Get Lucky"); err != nil {
		t.Fatal(err)
	}
	if _, err := u.HandleUtterance(context.Background(), "кто поёт?"); err != nil {
		t.Fatal(err)
	}
	second := llm.seen[len(llm.seen)-1]
	if len(second.Messages) != 3 || second.Messages[0].Content != "включи Get Lucky" || second.Messages[2].Content != "кто поёт?" {
		t.Fatalf("messages %+v", second.Messages)
	}
	if second.Messages[1].Role != "assistant" || second.Messages[1].Content != "ок" {
		t.Fatalf("assistant %+v", second.Messages[1])
	}
	u.memAt = time.Now().Add(-dialogueTTL - time.Second)
	if _, err := u.HandleUtterance(context.Background(), "ещё раз"); err != nil {
		t.Fatal(err)
	}
	last := llm.seen[len(llm.seen)-1]
	if len(last.Messages) != 1 || last.Messages[0].Content != "ещё раз" {
		t.Fatalf("expired %+v", last.Messages)
	}
}

func TestFollowUpDoesNotForceTools(t *testing.T) {
	_, words := exampleAssistant(t)
	bus := &memBus{
		tools: []domain.Tool{{Name: "play_music", Description: "музыка"}},
		hints: []string{"включи"},
		body:  "играет Get Lucky — Daft Punk",
	}
	llm := &fakeLLM{script: []domain.Completion{
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "play_music", Arguments: `{"query":"Get Lucky","kind":"track"}`}}},
		{Text: "Дафт Панк", Model: "fake"},
	}}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	if _, err := u.HandleUtterance(context.Background(), "включи Get Lucky"); err != nil {
		t.Fatal(err)
	}
	if _, err := u.HandleUtterance(context.Background(), "кто поёт?"); err != nil {
		t.Fatal(err)
	}
	last := llm.seen[len(llm.seen)-1]
	if last.ToolChoice != "auto" {
		t.Fatalf("choice %q", last.ToolChoice)
	}
	if last.Messages[0].Content != "включи Get Lucky" || last.Messages[len(last.Messages)-1].Content != "кто поёт?" {
		t.Fatalf("messages %+v", last.Messages)
	}
}

func TestHandleChatUsesGivenHistory(t *testing.T) {
	name, words := exampleAssistant(t)
	llm := &fakeLLM{text: "Дафт Панк"}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Prompt: "p"})
	_, err := u.HandleChat(context.Background(), []domain.Message{
		{Role: "system", Content: "чужой промпт"},
		{Role: "user", Content: name + ", включи Get Lucky"},
		{Role: "assistant", Content: "играет Get Lucky — Daft Punk"},
	}, "кто это?")
	if err != nil {
		t.Fatal(err)
	}
	got := llm.seen[0].Messages
	if len(got) != 3 || got[0].Content != "включи Get Lucky" || got[1].Content != "играет Get Lucky — Daft Punk" || got[2].Content != "кто это?" {
		t.Fatalf("messages %+v", got)
	}
}

func TestNamedStopSkipsTheModel(t *testing.T) {
	name, words := exampleAssistant(t)
	bus := &memBus{body: domain.SilentReply}
	llm := &fakeLLM{text: "останавливаю Get Lucky"}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	reply, err := u.HandleUtterance(context.Background(), name+", "+name+", стоп!")
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Quiet || reply.Text != "Остановилась." {
		t.Fatalf("reply %+v", reply)
	}
	if len(llm.seen) != 0 {
		t.Fatalf("model was asked: %+v", llm.seen)
	}
	if bus.pauses == 0 && bus.last.Name == "music_control" {
		t.Fatalf("bare stop only called music_control: %+v", bus.last)
	}
	if bus.pauses > 0 && bus.last.Name == "music_control" {
		t.Fatalf("bare stop still invoked music_control: %+v pauses=%d", bus.last, bus.pauses)
	}
}

func TestPlaySuccessStaysQuiet(t *testing.T) {
	bus := &memBus{
		tools: []domain.Tool{{Name: "play_music"}},
		hints: []string{"включи"},
		body:  "играет Get Lucky — Daft Punk",
	}
	llm := &fakeLLM{script: []domain.Completion{
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "play_music", Arguments: `{"query":"Get Lucky","kind":"track"}`}}, Model: "fake"},
	}}
	name, words := exampleAssistant(t)
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	reply, err := u.HandleUtterance(context.Background(), name+", включи Get Lucky")
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Quiet || !strings.HasPrefix(reply.Text, "играет ") {
		t.Fatalf("reply %+v", reply)
	}
	if len(llm.seen) != 1 {
		t.Fatalf("rounds %d", len(llm.seen))
	}
}

func TestAddressed(t *testing.T) {
	name, words := exampleAssistant(t)
	u := New(Deps{LLM: &fakeLLM{text: "x"}, Wake: fakeWake{words: words}})
	if !u.Addressed(name+", скажи да") || !u.Addressed(name) {
		t.Fatal("expected wake")
	}
	if u.Addressed("позитивная музыка") {
		t.Fatal("background speech must not count")
	}
}

func TestMoodAddendumOnLaterTurn(t *testing.T) {
	name, _ := exampleAssistant(t)
	dir := t.TempDir()
	skill, err := mood.New(mood.Settings{Database: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer skill.Close()
	base := "Ты " + name + ". Трек не называй."
	llm := &fakeLLM{text: "да"}
	u := New(Deps{LLM: llm, Prompt: base, MoodDir: dir})
	if _, err := u.HandleUtterance(context.Background(), "привет"); err != nil {
		t.Fatal(err)
	}
	if llm.seen[0].System != base {
		t.Fatalf("system %q", llm.seen[0].System)
	}
	confirm, err := skill.Ask(context.Background(), "стань злой")
	if err != nil {
		t.Fatal(err)
	}
	if confirm != "Хорошо, буду злой." {
		t.Fatalf("confirm %q", confirm)
	}
	if _, err := u.HandleUtterance(context.Background(), "ещё раз"); err != nil {
		t.Fatal(err)
	}
	angry, ok := mood.Prompt("злая")
	if !ok || !strings.HasPrefix(llm.seen[1].System, base) || !strings.Contains(llm.seen[1].System, angry) {
		t.Fatalf("system %q", llm.seen[1].System)
	}
	if _, err := skill.Ask(context.Background(), "милая"); err != nil {
		t.Fatal(err)
	}
	if _, err := u.HandleUtterance(context.Background(), "и ещё"); err != nil {
		t.Fatal(err)
	}
	sweet, _ := mood.Prompt("милая")
	if strings.Contains(llm.seen[2].System, angry) || !strings.Contains(llm.seen[2].System, sweet) {
		t.Fatalf("replaced system %q", llm.seen[2].System)
	}
}

func TestSetMoodSpeaksConfirmation(t *testing.T) {
	bus := &memBus{
		tools: []domain.Tool{{Name: "set_mood"}},
		hints: []string{"стань "},
		body:  "Хорошо, буду злой.",
	}
	llm := &fakeLLM{script: []domain.Completion{
		{ToolCalls: []domain.ToolCall{{ID: "1", Name: "set_mood", Arguments: `{"mood":"злой"}`}}, Model: "fake"},
		{Text: "Ну и злая я теперь, доволен?", Model: "fake"},
	}}
	name, words := exampleAssistant(t)
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
	reply, err := u.HandleUtterance(context.Background(), name+", стань злой")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "Хорошо, буду злой." {
		t.Fatalf("reply %q", reply.Text)
	}
	if len(llm.seen) != 1 {
		t.Fatalf("rounds %d", len(llm.seen))
	}
	if bus.last.Name != "set_mood" {
		t.Fatalf("invoke %+v", bus.last)
	}
}

type gateTTS struct {
	started chan struct{}
	goOn    chan struct{}
	once    sync.Once
	rel     sync.Once
}

func (g *gateTTS) Synthesize(_ context.Context, _, voice string) (domain.Audio, error) {
	g.once.Do(func() { close(g.started) })
	<-g.goOn
	return domain.Audio{MIME: "audio/wav", Bytes: []byte("RIFF"), Voice: voice}, nil
}

func (g *gateTTS) release() { g.rel.Do(func() { close(g.goOn) }) }

func TestStopDuringSpeechAbortsPlayback(t *testing.T) {
	name, words := exampleAssistant(t)
	phrases := []string{name + " стоп", name + ", стоп", "стоп", "хватит", "замолчи", "остановись", name + ", хватит"}
	for _, phrase := range phrases {
		t.Run(phrase, func(t *testing.T) {
			gate := &gateTTS{started: make(chan struct{}), goOn: make(chan struct{})}
			t.Cleanup(gate.release)
			bus := &memBus{}
			u := New(Deps{
				LLM: &fakeLLM{text: "нельзя"}, Wake: fakeWake{words: words},
				TTS: gate, Skills: bus, Voice: "baya", Prompt: "p",
			})
			audioCh := make(chan domain.Audio, 1)
			errCh := make(chan error, 1)
			go func() {
				audio, err := u.Speak(context.Background(), "Сегодня пятница.")
				audioCh <- audio
				errCh <- err
			}()
			select {
			case <-gate.started:
			case <-time.After(2 * time.Second):
				t.Fatal("speech did not start")
			}
			reply, err := u.HandleUtterance(context.Background(), phrase)
			if err != nil {
				t.Fatal(err)
			}
			if !reply.Quiet || reply.Text != domain.StoppedReply {
				t.Fatalf("reply %+v", reply)
			}
			// Wake pauses media; stop pauses again. Bare «стоп» only stops once.
			want := 1
			if domain.Heard(phrase, words) {
				want = 2
			}
			if bus.pauses != want {
				t.Fatalf("pauses %d want %d", bus.pauses, want)
			}
			if u.speaking() {
				t.Fatal("speech was not aborted")
			}
			gate.release()
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
			if audio := <-audioCh; len(audio.Bytes) != 0 {
				t.Fatalf("playback continued: %q", audio.Bytes)
			}
		})
	}
}

func TestPhraseDuringSpeechIsNotACommand(t *testing.T) {
	name, words := exampleAssistant(t)
	t.Run("while synthesizing", func(t *testing.T) {
		gate := &gateTTS{started: make(chan struct{}), goOn: make(chan struct{})}
		t.Cleanup(gate.release)
		llm := &fakeLLM{text: "нельзя"}
		u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, TTS: gate, Voice: "baya", Prompt: "p"})
		errCh := make(chan error, 1)
		audioCh := make(chan domain.Audio, 1)
		go func() {
			audio, err := u.Speak(context.Background(), "Сегодня пятница.")
			audioCh <- audio
			errCh <- err
		}()
		select {
		case <-gate.started:
		case <-time.After(2 * time.Second):
			t.Fatal("speech did not start")
		}
		for _, phrase := range []string{name + ", какая погода", "какая погода"} {
			_, err := u.HandleUtterance(context.Background(), phrase)
			if !errors.Is(err, domain.ErrEmpty) {
				t.Fatalf("%q err %v", phrase, err)
			}
		}
		if len(llm.seen) != 0 {
			t.Fatalf("phrase during speech called the model: %+v", llm.seen)
		}
		gate.release()
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
		if audio := <-audioCh; string(audio.Bytes) != "RIFF" {
			t.Fatalf("playback aborted by a normal phrase: %q", audio.Bytes)
		}
	})
	t.Run("while the column plays", func(t *testing.T) {
		llm := &fakeLLM{text: "нельзя"}
		bus := &memBus{}
		u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Skills: bus, Prompt: "p"})
		u.HoldSpeech(time.Minute)
		_, err := u.HandleUtterance(context.Background(), name+", какая погода")
		if !errors.Is(err, domain.ErrEmpty) {
			t.Fatalf("err %v", err)
		}
		if len(llm.seen) != 0 {
			t.Fatalf("model %+v", llm.seen)
		}
		reply, err := u.HandleUtterance(context.Background(), name+", стоп")
		if err != nil {
			t.Fatal(err)
		}
		// First wake while holding already paused; stop wakes and pauses again, then stopAll.
		if !reply.Quiet || reply.Text != domain.StoppedReply || bus.pauses < 2 {
			t.Fatalf("reply %+v pauses %d", reply, bus.pauses)
		}
		if u.speaking() {
			t.Fatal("ring hold stayed after stop")
		}
	})
}

func TestFollowUpSeesPreviousTurn(t *testing.T) {
	name, words := exampleAssistant(t)
	cases := []struct {
		user, reply, follow, stored string
	}{
		{name + ", включи эту", "Да, включить эту песню?", "да", "включи эту"},
		{name + ", включи эту", "Включить эту песню?", "эту", "включи эту"},
		{name + ", поставь его", "Ставлю его следующим.", "его", "поставь его"},
	}
	for _, tc := range cases {
		t.Run(tc.follow, func(t *testing.T) {
			llm := &fakeLLM{text: tc.reply}
			u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Prompt: "p"})
			if _, err := u.HandleUtterance(context.Background(), tc.user); err != nil {
				t.Fatal(err)
			}
			if _, err := u.HandleUtterance(context.Background(), tc.follow); err != nil {
				t.Fatal(err)
			}
			if len(llm.seen) != 2 {
				t.Fatalf("calls %d", len(llm.seen))
			}
			got := llm.seen[1].Messages
			if len(got) != 3 {
				t.Fatalf("messages %+v", got)
			}
			if got[0].Role != "user" || got[0].Content != tc.stored {
				t.Fatalf("stored user %+v", got[0])
			}
			if strings.Contains(strings.ToLower(got[0].Content), strings.ToLower(name)) {
				t.Fatalf("wake word stored: %q", got[0].Content)
			}
			if got[1].Role != "assistant" || got[1].Content != tc.reply {
				t.Fatalf("assistant %+v", got[1])
			}
			if got[2].Role != "user" || got[2].Content != tc.follow {
				t.Fatalf("follow %+v", got[2])
			}
			u.memAt = time.Now().Add(-dialogueTTL - time.Second)
			if _, err := u.HandleUtterance(context.Background(), "ещё"); err != nil {
				t.Fatal(err)
			}
			last := llm.seen[2].Messages
			if len(last) != 1 || last[0].Content != "ещё" {
				t.Fatalf("after ttl %+v", last)
			}
		})
	}
}

func TestDialogueKeepsEightMessages(t *testing.T) {
	cfg, err := config.LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	if dialogueKeep != cfg.Core.Dialogue.Keep {
		t.Fatalf("window %d", dialogueKeep)
	}
	llm := &fakeLLM{text: "ок"}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: cfg.Core.Wake.Words}, Prompt: "p"})
	for i := 0; i < 6; i++ {
		if _, err := u.HandleUtterance(context.Background(), fmt.Sprintf("фраза %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	last := llm.seen[len(llm.seen)-1].Messages
	if messagesContain(last, "фраза 0") {
		t.Fatalf("oldest stayed: %+v", last)
	}
	if !messagesContain(last, "фраза 1") || !messagesContain(last, "фраза 5") {
		t.Fatalf("window %+v", last)
	}
	u.memMu.Lock()
	stored := len(u.mem)
	u.memMu.Unlock()
	if stored != 8 {
		t.Fatalf("stored %d", stored)
	}
}

func TestHistoryOmitsSystemAndToolRoles(t *testing.T) {
	name, words := exampleAssistant(t)
	llm := &fakeLLM{text: "ок"}
	u := New(Deps{LLM: llm, Wake: fakeWake{words: words}, Prompt: "p"})
	_, err := u.HandleChat(context.Background(), []domain.Message{
		{Role: "system", Content: "чужой промпт"},
		{Role: "tool", ToolCallID: "1", Content: "секрет инструмента"},
		{Role: "user", Content: name + ", включи эту"},
		{Role: "assistant", Content: "Включила его."},
	}, "его")
	if err != nil {
		t.Fatal(err)
	}
	got := llm.seen[0].Messages
	if len(got) != 3 || got[0].Content != "включи эту" || got[1].Content != "Включила его." || got[2].Content != "его" {
		t.Fatalf("messages %+v", got)
	}
	for _, m := range got {
		if m.Role != "user" && m.Role != "assistant" {
			t.Fatalf("role %s", m.Role)
		}
		if strings.Contains(m.Content, "чужой") || strings.Contains(m.Content, "секрет") {
			t.Fatalf("leaked %q", m.Content)
		}
	}
	u.memMu.Lock()
	mem := append([]domain.Message(nil), u.mem...)
	u.memMu.Unlock()
	for _, m := range mem {
		if m.Role != "user" && m.Role != "assistant" {
			t.Fatalf("stored role %s", m.Role)
		}
		if strings.Contains(m.Content, "чужой") || strings.Contains(m.Content, "секрет") || strings.Contains(strings.ToLower(m.Content), strings.ToLower(name)) {
			t.Fatalf("stored %q", m.Content)
		}
	}
}

func TestAnswerSpeaksReply(t *testing.T) {
	name, _ := exampleAssistant(t)
	tts := &fakeTTS{}
	u := New(Deps{LLM: &fakeLLM{text: "привет"}, Wake: fakeWake{}, TTS: tts, Voice: "baya"})
	reply, audio, err := u.Answer(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "привет" || tts.got != "привет" {
		t.Fatalf("reply %q tts %q", reply.Text, tts.got)
	}
	if audio.Voice != "baya" || string(audio.Bytes) != "RIFF" {
		t.Fatalf("audio %+v", audio)
	}
}

type countTTS struct {
	n   int
	got string
}

func (c *countTTS) Synthesize(_ context.Context, text, voice string) (domain.Audio, error) {
	c.n++
	c.got = text
	return domain.Audio{MIME: "audio/wav", Bytes: []byte("RIFF-this-is-long-enough-wav-bytes!!"), Voice: voice}, nil
}

func TestSpeakHitsFixedPhraseCache(t *testing.T) {
	inner := &countTTS{}
	cached := repo.NewCachedTTS(inner, t.TempDir(), "baya")
	u := New(Deps{LLM: &fakeLLM{text: "x"}, Wake: fakeWake{}, TTS: cached, Voice: "baya"})
	if _, err := u.Speak(context.Background(), domain.LikedReply); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Speak(context.Background(), domain.LikedReply); err != nil {
		t.Fatal(err)
	}
	if inner.n != 1 || inner.got != domain.LikedReply {
		t.Fatalf("tts calls=%d got=%q", inner.n, inner.got)
	}
}
