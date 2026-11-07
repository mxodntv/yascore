package youtubemusic

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestLikeUnlikeKeepsOneRowAndBothTimes(t *testing.T) {
	db := openTemp(t)
	likedAt := time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC)
	playedAt := likedAt.Add(-2 * time.Minute)

	if err := db.Like("Get Lucky", "Daft Punk", "abc", ServiceYouTube, likedAt, playedAt); err != nil {
		t.Fatal(err)
	}
	likes, err := db.Likes()
	if err != nil {
		t.Fatal(err)
	}
	if len(likes) != 1 {
		t.Fatalf("likes %d", len(likes))
	}
	got := likes[0]
	if got.Title != "Get Lucky" || got.Artist != "Daft Punk" || got.Service != ServiceYouTube {
		t.Fatalf("track %+v", got.Track)
	}
	if !got.LikedAt.Equal(likedAt) || !got.LastPlayedAt.Equal(playedAt) {
		t.Fatalf("times liked %s played %s", got.LikedAt, got.LastPlayedAt)
	}

	later := likedAt.Add(time.Hour)
	if err := db.Like("Get Lucky", "Daft Punk", "abc", ServiceYouTube, later, playedAt); err != nil {
		t.Fatal(err)
	}
	likes, err = db.Likes()
	if err != nil {
		t.Fatal(err)
	}
	if len(likes) != 1 || !likes[0].LikedAt.Equal(later) || !likes[0].LastPlayedAt.Equal(playedAt) {
		t.Fatalf("second like %+v", likes)
	}

	removed, err := db.Unlike("Get Lucky", "Daft Punk", "abc", ServiceYouTube)
	if err != nil || !removed {
		t.Fatalf("unlike %v %v", removed, err)
	}
	likes, err = db.Likes()
	if err != nil {
		t.Fatal(err)
	}
	if len(likes) != 0 {
		t.Fatalf("still liked %+v", likes)
	}
}

func TestHistoryGrowsPastTwentyAndPreviousIsPriorPlay(t *testing.T) {
	db := openTemp(t)
	base := time.Date(2026, 11, 6, 21, 0, 0, 0, time.UTC)
	if err := db.AppendPlay("Alpha", "Band", "", ServiceYouTube, base); err != nil {
		t.Fatal(err)
	}
	if err := db.AppendPlay("Beta", "Band", "", ServiceYouTube, base.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	title, ok, err := db.PreviousTitle()
	if err != nil || !ok || title != "Alpha" {
		t.Fatalf("previous %q ok %v err %v", title, ok, err)
	}

	for i := 3; i <= 21; i++ {
		name := trackName(i)
		if err := db.AppendPlay(name, "Band", "", ServiceYouTube, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	plays, err := db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 21 {
		t.Fatalf("len %d", len(plays))
	}
	if plays[0].Title != "Alpha" || plays[len(plays)-1].Title != trackName(21) || !plays[len(plays)-1].Current {
		t.Fatalf("ends %+v ... %+v", plays[0], plays[len(plays)-1])
	}
	if plays[0].Service != ServiceYouTube || plays[0].PlayedAt.IsZero() {
		t.Fatalf("first play %+v", plays[0])
	}
	title, ok, err = db.PreviousTitle()
	if err != nil || !ok || title != trackName(20) {
		t.Fatalf("previous %q ok %v err %v", title, ok, err)
	}
}

func TestLegacyImportLeavesPlaceJSON(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "legacy")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "likes.json"), []byte(`{"likes":[{"id":"abc","title":"Get Lucky","artists":"Daft Punk"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "queue.json"), []byte(`{"tracks":[{"title":"Alpha","artists":"Band"},{"title":"Beta","artists":"Band"}],"index":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	place := filepath.Join(root, "skills", "weather", "data", "place.json")
	if err := os.MkdirAll(filepath.Dir(place), 0o755); err != nil {
		t.Fatal(err)
	}
	const body = "{\"city\":\"Киров\"}\n"
	if err := os.WriteFile(place, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	beside := filepath.Join(legacy, "place.json")
	if err := os.WriteFile(beside, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "yascore.db")
	db, err := OpenWithLegacy(path, legacy)
	if err != nil {
		t.Fatal(err)
	}
	likes, err := db.Likes()
	if err != nil {
		t.Fatal(err)
	}
	if len(likes) != 1 || likes[0].Title != "Get Lucky" || likes[0].Service != ServiceYouTube || likes[0].LikedAt.IsZero() || likes[0].LastPlayedAt.IsZero() {
		t.Fatalf("likes %+v", likes)
	}
	title, ok, err := db.PreviousTitle()
	if err != nil || !ok || title != "Alpha" {
		t.Fatalf("previous %q ok %v err %v", title, ok, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := OpenWithLegacy(path, legacy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	likes, err = again.Likes()
	if err != nil {
		t.Fatal(err)
	}
	if len(likes) != 1 {
		t.Fatalf("reimport %+v", likes)
	}
	got, err := os.ReadFile(place)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("place %s", got)
	}
	got, err = os.ReadFile(beside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("legacy place %s", got)
	}
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	db, err := OpenWithLegacy(filepath.Join(t.TempDir(), "yascore.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestCopiesPreviousDatabaseOnceAndLeavesIt(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "musicplayer.db")
	likedAt := "2026-11-06T18:04:05Z"
	playedAt := "2026-11-06T18:00:00Z"
	writePrevious(t, oldPath, likedAt, playedAt)

	path := filepath.Join(root, "yascore.db")
	db, err := OpenWithLegacy(path, filepath.Join(root, "missing-json"))
	if err != nil {
		t.Fatal(err)
	}
	likes, err := db.Likes()
	if err != nil {
		t.Fatal(err)
	}
	if len(likes) != 1 || likes[0].Title != "Get Lucky" || likes[0].Artist != "Daft Punk" || likes[0].Service != ServiceYouTube {
		t.Fatalf("likes %+v", likes)
	}
	if likes[0].LikedAt.Format(time.RFC3339) != likedAt || likes[0].LastPlayedAt.Format(time.RFC3339) != playedAt {
		t.Fatalf("times %+v", likes[0])
	}
	title, ok, err := db.PreviousTitle()
	if err != nil || !ok || title != "Alpha" {
		t.Fatalf("previous %q ok %v err %v", title, ok, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := OpenWithLegacy(path, filepath.Join(root, "missing-json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	likes, err = again.Likes()
	if err != nil {
		t.Fatal(err)
	}
	plays, err := again.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(likes) != 1 || len(plays) != 2 {
		t.Fatalf("likes %d plays %d", len(likes), len(plays))
	}
	if likes[0].LikedAt.Format(time.RFC3339) != likedAt {
		t.Fatalf("recopied times %+v", likes[0])
	}
	if countTable(t, oldPath, "musicplayer_likes") != 1 || countTable(t, oldPath, "musicplayer_plays") != 2 {
		t.Fatal("old file changed")
	}
}

func TestEmptyPreviousFileStillImportsJSON(t *testing.T) {
	root := t.TempDir()
	writePrevious(t, filepath.Join(root, "musicplayer.db"), "", "")
	legacy := filepath.Join(root, "legacy")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "likes.json"), []byte(`{"likes":[{"id":"abc","title":"Get Lucky","artists":"Daft Punk"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := OpenWithLegacy(filepath.Join(root, "yascore.db"), legacy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	likes, err := db.Likes()
	if err != nil {
		t.Fatal(err)
	}
	if len(likes) != 1 || likes[0].Title != "Get Lucky" {
		t.Fatalf("likes %+v", likes)
	}
	if countTable(t, filepath.Join(root, "musicplayer.db"), "musicplayer_likes") != 0 {
		t.Fatal("empty old file was written")
	}
}

func writePrevious(t *testing.T, path, likedAt, playedAt string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file://"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE musicplayer_likes (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    artist TEXT NOT NULL DEFAULT '',
    title_key TEXT NOT NULL,
    artist_key TEXT NOT NULL,
    service TEXT NOT NULL,
    liked_at TEXT NOT NULL,
    last_played_at TEXT NOT NULL,
    video_id TEXT NOT NULL DEFAULT '',
    UNIQUE (service, title_key, artist_key)
);
CREATE TABLE musicplayer_plays (
    id INTEGER PRIMARY KEY,
    seq INTEGER NOT NULL,
    title TEXT NOT NULL,
    artist TEXT NOT NULL DEFAULT '',
    title_key TEXT NOT NULL,
    artist_key TEXT NOT NULL,
    service TEXT NOT NULL,
    played_at TEXT NOT NULL,
    video_id TEXT NOT NULL DEFAULT '',
    current INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE musicplayer_recent (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    title TEXT NOT NULL DEFAULT '',
    artist TEXT NOT NULL DEFAULT '',
    title_key TEXT NOT NULL DEFAULT '',
    artist_key TEXT NOT NULL DEFAULT '',
    service TEXT NOT NULL DEFAULT '',
    video_id TEXT NOT NULL DEFAULT '',
    played_at TEXT NOT NULL DEFAULT ''
);`); err != nil {
		t.Fatal(err)
	}
	if likedAt == "" {
		return
	}
	if _, err := db.Exec(`
INSERT INTO musicplayer_likes (id, title, artist, title_key, artist_key, service, liked_at, last_played_at, video_id)
VALUES (1, 'Get Lucky', 'Daft Punk', 'get lucky', 'daft punk', 'youtube-music', ?, ?, 'abc')`, likedAt, playedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO musicplayer_plays (id, seq, title, artist, title_key, artist_key, service, played_at, video_id, current)
VALUES (1, 0, 'Alpha', 'Band', 'alpha', 'band', 'youtube-music', ?, '', 0),
       (2, 1, 'Beta', 'Band', 'beta', 'band', 'youtube-music', ?, '', 1)`, playedAt, likedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO musicplayer_recent (id, title, artist, title_key, artist_key, service, video_id, played_at)
VALUES (1, 'Beta', 'Band', 'beta', 'band', 'youtube-music', '', ?)`, likedAt); err != nil {
		t.Fatal(err)
	}
}

func countTable(t *testing.T, path, table string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file://"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
