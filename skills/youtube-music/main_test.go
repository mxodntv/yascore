package youtubemusic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"yascore/internal/domain"
)

func TestPlayMusicReturnsSearchIntent(t *testing.T) {
	s := Speaker{}
	got, err := s.handle(context.Background(), "play_music", `{"query":"Get Lucky","kind":"track"}`)
	if err != nil {
		t.Fatal(err)
	}
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(got), &intent) != nil || intent.Do != domain.MusicSearchPlay || intent.Query != "Get Lucky" {
		t.Fatalf("got %s", got)
	}
}

func TestVagueMusicReturnsResumeMixIntent(t *testing.T) {
	s := Speaker{}
	got, err := s.handle(context.Background(), "play_music", `{"query":"","kind":"any"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, domain.MusicResumeMix) {
		t.Fatalf("got %s", got)
	}
}

func TestPlaylistReturnsPlayIntent(t *testing.T) {
	s := Speaker{}
	got, err := s.handle(context.Background(), "play_music", `{"query":"jazz","kind":"playlist"}`)
	if err != nil {
		t.Fatal(err)
	}
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(got), &intent) != nil || intent.Do != domain.MusicPlay || intent.Phrase != "jazz" {
		t.Fatalf("got %s", got)
	}
}

func TestEmptyTrackIsVagueMusic(t *testing.T) {
	s := Speaker{}
	got, err := s.handle(context.Background(), "play_music", `{"query":"","kind":"track"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, domain.MusicResumeMix) {
		t.Fatalf("got %s", got)
	}
}

func TestPauseResumeAcknowledgeWithoutSSH(t *testing.T) {
	s := Speaker{}
	got, err := s.handle(context.Background(), "pause", "")
	if err != nil || got != "ok" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = s.handle(context.Background(), "resume", "")
	if err != nil || got != "ok" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestMusicControlPauseIsIntent(t *testing.T) {
	s := Speaker{}
	got, err := s.handle(context.Background(), "music_control", `{"action":"pause"}`)
	if err != nil {
		t.Fatal(err)
	}
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(got), &intent) != nil || intent.Do != domain.MusicControl || intent.Action != "pause" {
		t.Fatalf("got %s", got)
	}
}

func TestSoundActive(t *testing.T) {
	if !soundActive(`{"state":{"status":"playing"}}`) || !soundActive(`{"state":{"status":"buffering"}}`) {
		t.Fatal("playing")
	}
	if soundActive(`{"state":{"status":"paused"}}`) || soundActive(`{"state":{"status":"stopped"}}`) {
		t.Fatal("quiet")
	}
}
