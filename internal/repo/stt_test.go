package repo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPSTT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/transcribe" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "audio/wav" {
			t.Errorf("ctype %s", r.Header.Get("Content-Type"))
		}
		_, _ = w.Write([]byte(`{"text":"привет"}`))
	}))
	defer srv.Close()
	text, err := NewHTTPSTT(srv.URL, time.Second).Transcribe(context.Background(), []byte("RIFF"))
	if err != nil || text != "привет" {
		t.Fatalf("text %q err %v", text, err)
	}
}

func TestHTTPSTTEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"  "}`))
	}))
	defer srv.Close()
	_, err := NewHTTPSTT(srv.URL, time.Second).Transcribe(context.Background(), []byte("RIFF"))
	if err == nil || err.Error() != "empty_transcript" {
		t.Fatal(err)
	}
}
