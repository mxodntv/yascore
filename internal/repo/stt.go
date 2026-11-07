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
)

// HTTPSTT posts WAV to a faster-whisper worker.
type HTTPSTT struct {
	url    string
	client *http.Client
}

// NewHTTPSTT builds a client. base is the worker origin, without /v1/transcribe.
func NewHTTPSTT(base string, timeout time.Duration) *HTTPSTT {
	return &HTTPSTT{
		url:    strings.TrimRight(base, "/") + "/v1/transcribe",
		client: &http.Client{Timeout: timeout},
	}
}

// Transcribe implements domain.STT.
func (s *HTTPSTT) Transcribe(ctx context.Context, wav []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(wav))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "audio/wav")
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timeout: %w", err)
		}
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("stt_http: %s", bytes.TrimSpace(body))
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("stt decode: %w", err)
	}
	if strings.TrimSpace(parsed.Text) == "" {
		return "", fmt.Errorf("empty_transcript")
	}
	return strings.TrimSpace(parsed.Text), nil
}
