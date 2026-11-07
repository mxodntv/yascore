package domain

import "context"

// Audio is synthesized speech.
type Audio struct {
	MIME  string
	Bytes []byte
	Voice string
}

// TTS turns text into audio.
type TTS interface {
	Synthesize(ctx context.Context, text, voice string) (Audio, error)
}
