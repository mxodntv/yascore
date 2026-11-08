package musicyoutube

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yascore/internal/domain"
	"yascore/internal/music"
)

func TestLikeUnlikeAndPlaySavedTrack(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	place := filepath.Join(dir, "skills", "weather", "data", "place.json")
	if err := os.MkdirAll(filepath.Dir(place), 0o755); err != nil {
		t.Fatal(err)
	}
	const placeBody = "{\"country\":\"Россия\",\"city\":\"Киров\",\"latitude\":58.6,\"longitude\":49.66}\n"
	if err := os.WriteFile(place, []byte(placeBody), 0o644); err != nil {
		t.Fatal(err)
	}
	speaker := Speaker{music: openMusic(t)}

	got, err := speaker.handle(ctx, "music_control", `{"action":"like","title":"Get Lucky","artists":"Daft Punk","id":"abc"}`)
	if err != nil {
		t.Fatal(err)
	}
	assertSay(t, got, domain.LikedReply)
	got, err = speaker.handle(ctx, "music_control", `{"action":"like","title":"Get Lucky","artists":"Daft Punk","id":"abc"}`)
	if err != nil {
		t.Fatal(err)
	}
	assertSay(t, got, domain.LikedReply)
	state := readMusic(t, speaker)
	if len(state.Likes) != 1 || state.Likes[0].ID != "abc" || state.Likes[0].Title != "Get Lucky" {
		t.Fatalf("likes %+v", state.Likes)
	}

	got, err = speaker.handle(ctx, "play_music", `{"query":"","kind":"likes"}`)
	if err != nil {
		t.Fatal(err)
	}
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(got), &intent) != nil || intent.Do != domain.MusicSearchPlay {
		t.Fatalf("play likes %s", got)
	}
	if !strings.Contains(intent.Query, "Get Lucky") || !strings.Contains(intent.Query, "Daft Punk") {
		t.Fatalf("query %q", intent.Query)
	}

	got, err = speaker.handle(ctx, "music_control", `{"action":"unlike","title":"Get Lucky","artists":"Daft Punk","id":"abc"}`)
	if err != nil {
		t.Fatal(err)
	}
	assertSay(t, got, domain.UnlikedReply)
	state = readMusic(t, speaker)
	if len(state.Likes) != 0 {
		t.Fatalf("still liked %+v", state.Likes)
	}
	gotPlace, err := os.ReadFile(place)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotPlace) != placeBody {
		t.Fatalf("weather wiped: %s", gotPlace)
	}
}

func TestLikeUsesJustPlayedTrack(t *testing.T) {
	ctx := context.Background()
	speaker := Speaker{music: openMusic(t)}
	if err := speaker.save(ctx, musicState{Likes: []savedTrack{}, Recent: &savedTrack{ID: "old", Title: "Instant Crush", Artists: "Daft Punk"}}); err != nil {
		t.Fatal(err)
	}
	got, err := speaker.handle(ctx, "music_control", `{"action":"like"}`)
	if err != nil {
		t.Fatal(err)
	}
	assertSay(t, got, domain.LikedReply)
	state := readMusic(t, speaker)
	if len(state.Likes) != 1 || state.Likes[0].Title != "Instant Crush" {
		t.Fatalf("likes %+v", state.Likes)
	}
}

func TestEmptyLikesSaySo(t *testing.T) {
	ctx := context.Background()
	speaker := Speaker{music: openMusic(t)}
	for _, kind := range []string{"likes", "similar"} {
		got, err := speaker.handle(ctx, "play_music", `{"query":"","kind":"`+kind+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		assertSay(t, got, noLikesYet)
	}
	got, err := speaker.handle(ctx, "music_control", `{"action":"unlike"}`)
	if err != nil {
		t.Fatal(err)
	}
	assertSay(t, got, noLikesYet)
}

func TestSimilarUsesStoredArtist(t *testing.T) {
	ctx := context.Background()
	speaker := Speaker{music: openMusic(t)}
	if err := speaker.save(ctx, musicState{Likes: []savedTrack{{ID: "abc", Title: "Get Lucky", Artists: "Daft Punk"}}}); err != nil {
		t.Fatal(err)
	}
	got, err := speaker.handle(ctx, "play_music", `{"query":"","kind":"similar"}`)
	if err != nil {
		t.Fatal(err)
	}
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(got), &intent) != nil || intent.Do != domain.MusicSearchPlay || intent.Query != "Daft Punk" {
		t.Fatalf("similar %s", got)
	}
}

func TestNothingPlayingToLike(t *testing.T) {
	speaker := Speaker{music: openMusic(t)}
	got, err := speaker.handle(context.Background(), "music_control", `{"action":"like"}`)
	if err != nil {
		t.Fatal(err)
	}
	assertSay(t, got, nothingToLike)
}

func TestControlPreviousIsIntent(t *testing.T) {
	got, err := (&Speaker{}).handle(context.Background(), "music_control", `{"action":"previous"}`)
	if err != nil {
		t.Fatal(err)
	}
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(got), &intent) != nil || intent.Do != domain.MusicControl || intent.Action != "previous" {
		t.Fatalf("got %s", got)
	}
}

func assertSay(t *testing.T, raw, want string) {
	t.Helper()
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(raw), &intent) != nil || intent.Do != domain.MusicSay || intent.Text != want {
		t.Fatalf("want say %q got %s", want, raw)
	}
}

func openMusic(t *testing.T) *music.Store {
	t.Helper()
	db, err := music.Open(filepath.Join(t.TempDir(), "yascore.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func readMusic(t *testing.T, speaker Speaker) musicState {
	t.Helper()
	state, err := speaker.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return state
}
