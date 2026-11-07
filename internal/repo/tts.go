package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"yascore/internal/domain"
)

// HTTPTTS posts text to a Silero worker and reads WAV bytes.
type HTTPTTS struct {
	url        string
	voice      string
	sampleRate int
	client     *http.Client
}

// NewHTTPTTS builds a client. base is the worker origin.
func NewHTTPTTS(base, voice string, sampleRate int, timeout time.Duration) *HTTPTTS {
	if sampleRate == 0 {
		sampleRate = 48000
	}
	return &HTTPTTS{
		url:        strings.TrimRight(base, "/") + "/v1/speak",
		voice:      voice,
		sampleRate: sampleRate,
		client:     &http.Client{Timeout: timeout},
	}
}

// Synthesize implements domain.TTS.
func (t *HTTPTTS) Synthesize(ctx context.Context, text, voice string) (domain.Audio, error) {
	if voice == "" {
		voice = t.voice
	}
	raw, _ := json.Marshal(map[string]any{
		"text":        text,
		"voice":       voice,
		"sample_rate": t.sampleRate,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(raw))
	if err != nil {
		return domain.Audio{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return domain.Audio{}, fmt.Errorf("timeout: %w", err)
		}
		return domain.Audio{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return domain.Audio{}, fmt.Errorf("tts_http: %s", bytes.TrimSpace(body))
	}
	if len(body) < 32 {
		return domain.Audio{}, fmt.Errorf("tts_empty")
	}
	mime := resp.Header.Get("Content-Type")
	if mime == "" {
		mime = "audio/wav"
	}
	return domain.Audio{MIME: mime, Bytes: body, Voice: voice}, nil
}
