//go:build integration

package usecase_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yascore/autotests/stats"
	"yascore/internal/domain"
	"yascore/internal/repo"
	"yascore/internal/usecase"
)

type wake struct{ words []string }

func (w wake) Strip(text string) string { return domain.Strip(text, w.words) }

func TestCoreText(t *testing.T) {
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"fake","choices":[{"message":{"content":"да"}}]}`))
	}))
	defer llmSrv.Close()
	ttsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFF-this-is-long-enough-wav-bytes!!"))
	}))
	defer ttsSrv.Close()

	name, words := exampleAssistant(t)
	prompt := "Ты " + name + "."
	u := usecase.New(usecase.Deps{
		LLM:    repo.NewOllama(llmSrv.URL+"/v1", "fake", time.Second),
		Wake:   wake{words: words},
		TTS:    repo.NewHTTPTTS(ttsSrv.URL, "baya", 48000, time.Second),
		Prompt: prompt, Voice: "baya", Model: "fake",
	})
	start := time.Now()
	reply, audio, err := u.Answer(context.Background(), name+", скажи да")
	elapsed := time.Since(start).Milliseconds()
	rec := stats.Record{
		Suite: "core-text", N: 1, Pass: 1,
		Stages: map[string]stats.Stage{
			"llm": stats.FromSamples([]int64{elapsed}),
			"tts": stats.FromSamples([]int64{elapsed}),
			"e2e": stats.FromSamples([]int64{elapsed}),
		},
	}
	if err != nil || reply.Text != "да" || len(audio.Bytes) < 32 {
		rec.Pass, rec.Fail = 0, 1
		rec.Errors = []stats.ErrCount{{Code: "e2e", Count: 1}}
	}
	if pubErr := stats.Publish(rec); pubErr != nil {
		t.Fatal(pubErr)
	}
}
