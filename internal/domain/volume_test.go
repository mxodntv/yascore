package domain

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestDuckPercent(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 0}, {-3, 0}, {1, 1}, {50, 1}, {80, 1}, {100, 1}, {140, 1},
	}
	for _, tc := range cases {
		if got := DuckPercent(tc.in); got != tc.want {
			t.Fatalf("DuckPercent(%d)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestVoiceScale(t *testing.T) {
	if VoiceScale(0) != 1 || VoiceScale(100) != 1 {
		t.Fatal("full scale")
	}
	if VoiceScale(70) != 0.7 {
		t.Fatalf("scale %v", VoiceScale(70))
	}
}

func TestWAVDuration(t *testing.T) {
	// 8000 Hz, mono, 16-bit, 8000 frames = 1s. The data bytes themselves are not required.
	b := make([]byte, 44)
	copy(b[0:4], "RIFF")
	binary.LittleEndian.PutUint32(b[4:8], 36+16000)
	copy(b[8:12], "WAVE")
	copy(b[12:16], "fmt ")
	binary.LittleEndian.PutUint32(b[16:20], 16)
	binary.LittleEndian.PutUint16(b[20:22], 1)
	binary.LittleEndian.PutUint16(b[22:24], 1)
	binary.LittleEndian.PutUint32(b[24:28], 8000)
	binary.LittleEndian.PutUint32(b[28:32], 16000)
	binary.LittleEndian.PutUint16(b[32:34], 2)
	binary.LittleEndian.PutUint16(b[34:36], 16)
	copy(b[36:40], "data")
	binary.LittleEndian.PutUint32(b[40:44], 16000)
	if got := WAVDuration(b); got != time.Second {
		t.Fatalf("duration %s", got)
	}
	if WAVDuration([]byte("nope")) != 0 {
		t.Fatal("non-wav")
	}
}
