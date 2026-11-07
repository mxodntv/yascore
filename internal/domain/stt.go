package domain

import "context"

// STT turns a WAV blob into text.
type STT interface {
	Transcribe(ctx context.Context, wav []byte) (string, error)
}
