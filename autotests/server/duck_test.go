//go:build integration

package server_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
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

func TestSpeakDucksSkillThenRestores(t *testing.T) {
	ttsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(pcmWav(8000, 800))
	}))
	defer ttsSrv.Close()

	hub := repo.NewHub()
	u := usecase.New(usecase.Deps{
		LLM: quietLLM{}, Wake: passWake{}, TTS: repo.NewHTTPTTS(ttsSrv.URL, "baya", 8000, 2*time.Second),
		Prompt: "p", Voice: "baya",
	})
	name, _ := exampleAssistant(t)
	edge := server.New(u, hub, server.Info{Assistant: name, VoiceVolume: 100}, t.TempDir(), "http://127.0.0.1:8091")
	edge.UseDucking(usecase.NewDucker(hub))
	ts := httptest.NewServer(edge.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
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
	var sets []string
	go func() {
		for ctx.Err() == nil {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg domain.BusMessage
			if json.Unmarshal(raw, &msg) != nil || msg.Type != "level" {
				continue
			}
			content := `{"percent":80,"active":true}`
			if msg.Arguments != "" {
				mu.Lock()
				sets = append(sets, msg.Arguments)
				mu.Unlock()
				content = "ok"
			}
			out, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: content})
			_ = conn.Write(ctx, websocket.MessageText, out)
		}
	}()

	start := time.Now()
	resp, err := http.Post(ts.URL+"/v1/speak", "application/json", strings.NewReader(`{"text":"да"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("speak %s", resp.Status)
	}
	deadline := time.Now().Add(8 * time.Second)
	var got []string
	var duckedAt time.Time
	for time.Now().Before(deadline) {
		mu.Lock()
		got = append([]string(nil), sets...)
		mu.Unlock()
		if duckedAt.IsZero() && len(got) > 0 && got[0] == "1" {
			duckedAt = time.Now()
		}
		if len(got) >= 2 && got[len(got)-1] == "80" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	rec := stats.Record{Suite: "duck", N: 1, Pass: 1, Stages: map[string]stats.Stage{
		"e2e":  stats.FromSamples([]int64{duckedAt.Sub(start).Milliseconds()}),
		"hold": stats.FromSamples([]int64{time.Since(start).Milliseconds()}),
	}}
	if len(got) < 2 || got[0] != "1" || got[len(got)-1] != "80" {
		rec.Pass, rec.Fail = 0, 1
		rec.Errors = []stats.ErrCount{{Code: "duck_levels", Count: 1}}
		t.Errorf("levels %v", got)
	}
	for _, level := range got[:len(got)-1] {
		if level != "1" {
			t.Errorf("levels %v", got)
			break
		}
	}
	if pubErr := stats.Publish(rec); pubErr != nil {
		t.Fatal(pubErr)
	}
}

type quietLLM struct{}

func (quietLLM) Complete(context.Context, domain.CompleteRequest) (domain.Completion, error) {
	return domain.Completion{Text: "да", Model: "fake"}, nil
}

func pcmWav(rate, frames int) []byte {
	data := frames * 2
	b := make([]byte, 44+data)
	copy(b[0:4], "RIFF")
	binary.LittleEndian.PutUint32(b[4:8], uint32(36+data))
	copy(b[8:12], "WAVE")
	copy(b[12:16], "fmt ")
	binary.LittleEndian.PutUint32(b[16:20], 16)
	binary.LittleEndian.PutUint16(b[20:22], 1)
	binary.LittleEndian.PutUint16(b[22:24], 1)
	binary.LittleEndian.PutUint32(b[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(b[28:32], uint32(rate*2))
	binary.LittleEndian.PutUint16(b[32:34], 2)
	binary.LittleEndian.PutUint16(b[34:36], 16)
	copy(b[36:40], "data")
	binary.LittleEndian.PutUint32(b[40:44], uint32(data))
	return b
}
