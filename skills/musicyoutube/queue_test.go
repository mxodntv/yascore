package musicyoutube

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"yascore/internal/domain"
)

func TestQueuePreviousThenNextIntents(t *testing.T) {
	ctx := context.Background()
	speaker := Speaker{music: openMusic(t)}
	if err := speaker.save(ctx, musicState{
		Tracks: []savedTrack{{Title: "Alpha"}, {Title: "Beta"}},
		Index:  1,
		Recent: &savedTrack{Title: "Beta"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := speaker.handle(ctx, "music_control", `{"action":"previous"}`)
	if err != nil {
		t.Fatal(err)
	}
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(got), &intent) != nil || intent.Do != domain.MusicSearchPlay || intent.Query != "Alpha" || !intent.Follow {
		t.Fatalf("previous %s", got)
	}
	state := readMusic(t, speaker)
	if state.Index != 0 || state.Tracks[0].Title != "Alpha" {
		t.Fatalf("after previous %+v index %d", state.Tracks, state.Index)
	}

	got, err = speaker.handle(ctx, "music_control", `{"action":"next"}`)
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal([]byte(got), &intent) != nil || intent.Do != domain.MusicSearchPlay || intent.Query != "Beta" {
		t.Fatalf("next %s", got)
	}
	state = readMusic(t, speaker)
	if state.Index != 1 {
		t.Fatalf("after next index %d", state.Index)
	}
}

func TestAdvanceIntent(t *testing.T) {
	ctx := context.Background()
	speaker := Speaker{music: openMusic(t)}
	if err := speaker.save(ctx, musicState{
		Tracks: []savedTrack{{Title: "Alpha"}, {Title: "Beta"}},
		Index:  0,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := speaker.handle(ctx, "music_control", `{"action":"advance"}`)
	if err != nil {
		t.Fatal(err)
	}
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(got), &intent) != nil || intent.Query != "Beta" {
		t.Fatalf("advance %s", got)
	}
}

func TestPlayHistoryGrowsPastTwenty(t *testing.T) {
	ctx := context.Background()
	speaker := Speaker{music: openMusic(t)}
	state := musicState{Likes: []savedTrack{{ID: "like", Title: "Get Lucky", Artists: "Daft Punk"}}}
	const played = 21
	for i := 1; i <= played; i++ {
		state.Tracks = append(state.Tracks, savedTrack{Title: trackName(i)})
		state.Index = i - 1
	}
	if err := speaker.save(ctx, state); err != nil {
		t.Fatal(err)
	}
	got := readMusic(t, speaker)
	if len(got.Tracks) != played || got.Index != played-1 {
		t.Fatalf("len %d index %d", len(got.Tracks), got.Index)
	}
	if len(got.Likes) != 1 || got.Likes[0].Title != "Get Lucky" {
		t.Fatalf("likes %+v", got.Likes)
	}
}

func TestLikesSurviveQueue(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	place := filepath.Join(dir, "skills", "weather", "data", "place.json")
	if err := os.MkdirAll(filepath.Dir(place), 0o755); err != nil {
		t.Fatal(err)
	}
	const placeBody = "{\"country\":\"Россия\",\"city\":\"Киров\"}\n"
	if err := os.WriteFile(place, []byte(placeBody), 0o644); err != nil {
		t.Fatal(err)
	}
	speaker := Speaker{music: openMusic(t)}
	if err := speaker.save(ctx, musicState{
		Likes:  []savedTrack{{ID: "like", Title: "Get Lucky", Artists: "Daft Punk"}},
		Tracks: []savedTrack{{Title: "Instant Crush"}},
		Index:  0,
	}); err != nil {
		t.Fatal(err)
	}
	state := readMusic(t, speaker)
	if len(state.Likes) != 1 || state.Likes[0].ID != "like" {
		t.Fatalf("likes %+v", state.Likes)
	}
	gotPlace, err := os.ReadFile(place)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotPlace) != placeBody {
		t.Fatalf("weather %s", gotPlace)
	}
}

func TestQuietSuccessMarkerUnchanged(t *testing.T) {
	if domain.SilentReply != "[[varya:silent]]" {
		t.Fatalf("marker %s", domain.SilentReply)
	}
}

func trackName(i int) string {
	return fmt.Sprintf("Track-%02d", i)
}
