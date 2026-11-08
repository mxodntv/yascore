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

	"yascore/internal/domain"
	"yascore/internal/repo"
	"yascore/internal/server"
	"yascore/internal/usecase"
)

// orderBoard is the playing skill. Speech ducks it from /v1/speak, not from the reply text.
type orderBoard struct {
	mu      sync.Mutex
	percent int
	sets    []int
}

func (b *orderBoard) Levels(context.Context) []domain.SoundLevel {
	b.mu.Lock()
	defer b.mu.Unlock()
	return []domain.SoundLevel{{Name: "musicyoutube", Percent: b.percent, Active: true}}
}

func (b *orderBoard) SetLevel(_ context.Context, _ string, percent int) error {
	b.mu.Lock()
	b.percent = percent
	b.sets = append(b.sets, percent)
	b.mu.Unlock()
	return nil
}

func (b *orderBoard) now() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.percent
}

func TestUtteranceDoesNotSetVolumeToOne(t *testing.T) {
	board := &orderBoard{percent: 80}
	ttsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(shortWAV())
	}))
	defer ttsSrv.Close()

	u := usecase.New(usecase.Deps{
		LLM: llm{text: "ясно"}, Wake: wake{}, TTS: repo.NewHTTPTTS(ttsSrv.URL, "baya", 8000, 2*time.Second),
		Prompt: "p", Voice: "baya",
	})
	edge := server.New(u, nil, server.Info{VoiceVolume: 100}, t.TempDir(), "http://127.0.0.1:8091")
	edge.UseDucking(usecase.NewDucker(board))
	ts := httptest.NewServer(edge.Handler())
	defer ts.Close()

	name, _ := exampleAssistant(t)
	payload, err := json.Marshal(map[string]string{"text": name + ", какая погода"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/v1/utterance", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("utterance %s", resp.Status)
	}
	if board.now() != 80 || len(board.sets) != 0 {
		t.Fatalf("volume changed to %d via %v", board.now(), board.sets)
	}
	for _, level := range board.sets {
		if level == 1 {
			t.Fatalf("volume set to 1 percent: %v", board.sets)
		}
	}
}

func shortWAV() []byte {
	const rate, frames = 8000, 800
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
