package domain

import (
	"context"
	"encoding/binary"
	"time"
)

// SkillDuckPercent is how loud a playing skill stays while the assistant speaks,
// in percent of its current level. Configure sets it from core.duck.percent.
var SkillDuckPercent int

// Configure sets the duck percent from the loaded config.
func Configure(duckPercent int) {
	SkillDuckPercent = duckPercent
}

// SoundLevel is one skill that can make sound. Percent is 0–100.
type SoundLevel struct {
	Name    string `json:"name,omitempty"`
	Percent int    `json:"percent"`
	Active  bool   `json:"active"`
}

// SoundBoard is the set of skills that can be ducked while the assistant speaks.
type SoundBoard interface {
	Levels(ctx context.Context) []SoundLevel
	SetLevel(ctx context.Context, name string, percent int) error
}

// DuckPercent returns 1% of the current level, rounded to the nearest percent.
// A playing level that would round to silence stays at 1, the smallest step the player accepts.
func DuckPercent(current int) int {
	if current <= 0 {
		return 0
	}
	if current > 100 {
		current = 100
	}
	got := (current*SkillDuckPercent + 50) / 100
	if got < 1 {
		return 1
	}
	return got
}

// VoiceScale is the linear gain for Yasmina's file. 0 and 100 both mean full scale.
func VoiceScale(percent int) float64 {
	if percent <= 0 || percent >= 100 {
		return 1
	}
	return float64(percent) / 100
}

// WAVDuration reads a PCM WAV header. It returns 0 when the header is not a WAV.
func WAVDuration(b []byte) time.Duration {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0
	}
	var rate uint32
	var channels uint16
	var bits uint16
	var dataBytes uint32
	var haveFmt, haveData bool
	i := 12
	for i+8 <= len(b) {
		id := string(b[i : i+4])
		size := binary.LittleEndian.Uint32(b[i+4 : i+8])
		i += 8
		if id == "fmt " && size >= 16 && i+16 <= len(b) {
			channels = binary.LittleEndian.Uint16(b[i : i+2])
			rate = binary.LittleEndian.Uint32(b[i+4 : i+8])
			bits = binary.LittleEndian.Uint16(b[i+14 : i+16])
			haveFmt = true
		}
		if id == "data" {
			dataBytes = size
			haveData = true
			break
		}
		if i+int(size) > len(b) {
			break
		}
		i += int(size)
		if size%2 == 1 {
			i++
		}
	}
	if !haveFmt || !haveData || rate == 0 || channels == 0 || bits < 8 {
		return 0
	}
	frame := uint32(channels) * uint32(bits) / 8
	if frame == 0 {
		return 0
	}
	seconds := float64(dataBytes) / float64(frame) / float64(rate)
	return time.Duration(seconds * float64(time.Second))
}

// AudioIO plays synthesized speech on this PC. The microphone stays with maind.
type AudioIO interface {
	Play(ctx context.Context, audio Audio) error
}
