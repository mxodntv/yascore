package repo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPTTS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), "привет") {
			t.Errorf("body %s", raw)
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFF-this-is-long-enough-wav-bytes!!"))
	}))
	defer srv.Close()
	audio, err := NewHTTPTTS(srv.URL, "baya", 48000, time.Second).Synthesize(context.Background(), "привет", "")
	if err != nil {
		t.Fatal(err)
	}
	if audio.Voice != "baya" || audio.MIME != "audio/wav" || len(audio.Bytes) < 32 {
		t.Fatalf("%+v", audio)
	}
}

func TestHTTPTTSEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()
	_, err := NewHTTPTTS(srv.URL, "baya", 0, time.Second).Synthesize(context.Background(), "привет", "baya")
	if err == nil || err.Error() != "tts_empty" {
		t.Fatal(err)
	}
}
