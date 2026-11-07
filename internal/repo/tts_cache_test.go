package repo

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"yascore/internal/domain"
)

type countingTTS struct {
	n atomic.Int32
}

func (c *countingTTS) Synthesize(_ context.Context, text, voice string) (domain.Audio, error) {
	c.n.Add(1)
	body := []byte("RIFF-this-is-long-enough-wav-bytes!!" + text)
	return domain.Audio{MIME: "audio/wav", Bytes: body, Voice: voice}, nil
}

func TestCachedTTSStoresFixedPhraseAndHits(t *testing.T) {
	dir := t.TempDir()
	inner := &countingTTS{}
	tts := NewCachedTTS(inner, dir, "baya")
	text := domain.LikedReply

	first, err := tts.Synthesize(context.Background(), text, "baya")
	if err != nil {
		t.Fatal(err)
	}
	if inner.n.Load() != 1 {
		t.Fatalf("calls %d", inner.n.Load())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".wav" {
		t.Fatalf("cache %v %v", entries, err)
	}

	second, err := tts.Synthesize(context.Background(), text, "baya")
	if err != nil {
		t.Fatal(err)
	}
	if inner.n.Load() != 1 {
		t.Fatalf("second call hit silero: %d", inner.n.Load())
	}
	if string(first.Bytes) != string(second.Bytes) {
		t.Fatal("cached bytes differ")
	}
}

func TestCachedTTSDoesNotStoreFreeform(t *testing.T) {
	dir := t.TempDir()
	inner := &countingTTS{}
	tts := NewCachedTTS(inner, dir, "baya")
	if _, err := tts.Synthesize(context.Background(), "сегодня пятница и ясно", "baya"); err != nil {
		t.Fatal(err)
	}
	if _, err := tts.Synthesize(context.Background(), "сегодня пятница и ясно", "baya"); err != nil {
		t.Fatal(err)
	}
	if inner.n.Load() != 2 {
		t.Fatalf("freeform should not cache: %d", inner.n.Load())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected files %v", entries)
	}
}
