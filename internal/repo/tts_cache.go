package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"yascore/internal/domain"
)

// CachedTTS serves fixed (and previously stored) phrases from WAV files.
// A miss synthesizes through the inner TTS once; fixed phrases are written to dir.
type CachedTTS struct {
	inner domain.TTS
	dir   string
	voice string
}

// NewCachedTTS wraps inner. dir is usually data/tts-cache. voice is the default speaker.
func NewCachedTTS(inner domain.TTS, dir, voice string) *CachedTTS {
	return &CachedTTS{inner: inner, dir: dir, voice: voice}
}

// Synthesize implements domain.TTS.
// A cache hit skips Silero. Fixed phrases are stored on the first miss.
// Other text is synthesized and not written.
func (c *CachedTTS) Synthesize(ctx context.Context, text, voice string) (domain.Audio, error) {
	if c == nil || c.inner == nil {
		return domain.Audio{}, fmt.Errorf("tts is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.Audio{}, domain.ErrEmpty
	}
	if voice == "" {
		voice = c.voice
	}
	path := c.path(voice, text)
	if raw, err := os.ReadFile(path); err == nil && len(raw) >= 32 {
		return domain.Audio{MIME: "audio/wav", Bytes: raw, Voice: voice}, nil
	}
	audio, err := c.inner.Synthesize(ctx, text, voice)
	if err != nil {
		return domain.Audio{}, err
	}
	if domain.IsFixedPhrase(text) && len(audio.Bytes) >= 32 {
		_ = c.store(path, audio.Bytes)
	}
	return audio, nil
}

func (c *CachedTTS) path(voice, text string) string {
	sum := sha256.Sum256([]byte(voice + "\x00" + text))
	name := hex.EncodeToString(sum[:16]) + ".wav"
	return filepath.Join(c.dir, name)
}

func (c *CachedTTS) store(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
