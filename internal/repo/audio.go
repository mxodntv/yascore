package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"yascore/internal/domain"
)

// Pulse plays through ffplay or aplay. The microphone stays with maind.
type Pulse struct {
	Input  string
	Output string
}

// NewPulse builds a PC audio adapter. "default" uses the system device.
func NewPulse(input, output string) *Pulse {
	return &Pulse{Input: input, Output: output}
}

// Play writes audio to a temp file and plays it.
func (p *Pulse) Play(ctx context.Context, audio domain.Audio) error {
	if len(audio.Bytes) == 0 {
		return errors.New("tts_empty")
	}
	ext := ".wav"
	if bytes.Contains([]byte(audio.MIME), []byte("mpeg")) {
		ext = ".mp3"
	}
	f, err := os.CreateTemp("", "yascore-*"+ext)
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(audio.Bytes); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	var cmd *exec.Cmd
	if _, lookErr := exec.LookPath("ffplay"); lookErr == nil {
		cmd = exec.CommandContext(ctx, "ffplay", "-nodisp", "-autoexit", "-loglevel", "error", name)
	} else if ext == ".wav" {
		args := []string{name}
		if p != nil && p.Output != "" && p.Output != "default" {
			args = []string{"-D", p.Output, name}
		}
		cmd = exec.CommandContext(ctx, "aplay", args...)
	} else {
		return errors.New("no audio player")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("play: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}
