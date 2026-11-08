package music

import (
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "yascore.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestLikeRefreshesDateAndTitleArtistIdentity(t *testing.T) {
	db := openTemp(t)
	likedAt := time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC)
	again := likedAt.Add(time.Hour)
	if err := db.Like("Get Lucky", "Daft Punk", "abc", likedAt, likedAt); err != nil {
		t.Fatal(err)
	}
	// Same title+artist, different platform id and Topic suffix — one like card; liked_at moves.
	if err := db.Like("Get Lucky", "Daft Punk - Topic", "xyz", again, likedAt); err != nil {
		t.Fatal(err)
	}
	likes, err := db.Likes()
	if err != nil {
		t.Fatal(err)
	}
	if len(likes) != 1 || !likes[0].LikedAt.Equal(again) || likes[0].Artist != "Daft Punk" {
		t.Fatalf("likes %+v", likes)
	}
	if likes[0].ExternalID != "xyz" {
		t.Fatalf("external_id %q", likes[0].ExternalID)
	}
}

func TestAppendPlayStoresSkillAndGrowsCount(t *testing.T) {
	db := openTemp(t)
	base := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)
	if err := db.AppendPlay("Монетка", "ЛСП", "k9", SkillYouTube, base); err != nil {
		t.Fatal(err)
	}
	if err := db.AppendPlay("Монетка", "ЛСП", "k9", SkillYouTube, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	plays, err := db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 2 || plays[1].Skill != SkillYouTube || plays[1].ExternalID != "k9" {
		t.Fatalf("plays %+v", plays)
	}
	hist, err := db.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[0].Skill != SkillYouTube || hist[0].ListenedMS != 0 {
		t.Fatalf("history %+v", hist)
	}
	var count int
	if err := db.db.QueryRow(`SELECT play_count FROM music_tracks WHERE title_key = 'монетка'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("play_count %d", count)
	}
}

func TestRecordStartQueueAutoSkipsPlayWhenCurrentMatches(t *testing.T) {
	db := openTemp(t)
	base := time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC)
	if err := db.AppendPlay("A", "X", "1", SkillYouTube, base); err != nil {
		t.Fatal(err)
	}
	if err := db.AppendPlay("B", "Y", "2", SkillYouTube, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Skill moved current back to A via Save.
	if err := db.Save(Snapshot{
		Plays: []Track{{Title: "A", Artist: "X", ExternalID: "1"}, {Title: "B", Artist: "Y", ExternalID: "2"}},
		Index: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordStart("A", "X", "1", SkillYouTube, base.Add(2*time.Minute), QueueAuto); err != nil {
		t.Fatal(err)
	}
	plays, err := db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 2 || !plays[0].Current {
		t.Fatalf("plays %+v", plays)
	}
	hist, err := db.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 3 {
		t.Fatalf("history len %d", len(hist))
	}
	var count int
	if err := db.db.QueryRow(`SELECT play_count FROM music_tracks WHERE title_key = 'a'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("play_count A %d", count)
	}
}

func TestFinishListenUpdatesOpenHistory(t *testing.T) {
	db := openTemp(t)
	base := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	if err := db.AppendPlay("Монетка", "ЛСП", "k9", SkillYouTube, base); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishListen(95025); err != nil {
		t.Fatal(err)
	}
	hist, err := db.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].ListenedMS != 95025 {
		t.Fatalf("history %+v", hist)
	}
}

func TestEnsureQueuedAppendsWithoutMovingCurrent(t *testing.T) {
	db := openTemp(t)
	base := time.Date(2026, 11, 8, 5, 0, 0, 0, time.UTC)
	if err := db.AppendPlay("A", "X", "a", SkillYouTube, base); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureQueued([]Track{
		{Title: "B", Artist: "Y", ExternalID: "b"},
		{Title: "C", Artist: "Z", ExternalID: "c"},
	}, SkillYouTube); err != nil {
		t.Fatal(err)
	}
	plays, err := db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 3 || !plays[0].Current || plays[1].Current || plays[2].Current {
		t.Fatalf("plays %+v", plays)
	}
	if plays[1].ExternalID != "b" || plays[2].Title != "C" {
		t.Fatalf("queued %+v", plays)
	}
	hist, err := db.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("history should stay on started track only, got %d", len(hist))
	}
	// Idempotent: same queue again does not duplicate.
	if err := db.EnsureQueued([]Track{{Title: "B", Artist: "Y", ExternalID: "b"}}, SkillYouTube); err != nil {
		t.Fatal(err)
	}
	plays, err = db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 3 {
		t.Fatalf("dup plays %d", len(plays))
	}
}

func TestRecordStartQueuePromoteMovesCurrent(t *testing.T) {
	db := openTemp(t)
	base := time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)
	if err := db.AppendPlay("A", "X", "a", SkillYouTube, base); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureQueued([]Track{
		{Title: "B", Artist: "Y", ExternalID: "b"},
		{Title: "C", Artist: "Z", ExternalID: "c"},
	}, SkillYouTube); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordStart("B", "Y", "b", SkillYouTube, base.Add(time.Minute), QueuePromote); err != nil {
		t.Fatal(err)
	}
	plays, err := db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 3 {
		t.Fatalf("plays grew: %+v", plays)
	}
	if plays[0].Current || !plays[1].Current || plays[2].Current {
		t.Fatalf("current should be B: %+v", plays)
	}
	hist, err := db.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[1].Title != "B" {
		t.Fatalf("history %+v", hist)
	}
	if err := db.RecordStart("C", "Z", "c", SkillYouTube, base.Add(2*time.Minute), QueuePromote); err != nil {
		t.Fatal(err)
	}
	plays, err = db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 3 || !plays[2].Current {
		t.Fatalf("promote C: %+v", plays)
	}
	// Unknown track appends.
	if err := db.RecordStart("D", "W", "d", SkillYouTube, base.Add(3*time.Minute), QueuePromote); err != nil {
		t.Fatal(err)
	}
	plays, err = db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 4 || !plays[3].Current || plays[3].ExternalID != "d" {
		t.Fatalf("append unknown: %+v", plays)
	}
}

func TestRecordStartQueuePromotePrev(t *testing.T) {
	db := openTemp(t)
	base := time.Date(2026, 11, 8, 7, 0, 0, 0, time.UTC)
	if err := db.AppendPlay("A", "X", "a", SkillYouTube, base); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureQueued([]Track{
		{Title: "B", Artist: "Y", ExternalID: "b"},
		{Title: "C", Artist: "Z", ExternalID: "c"},
	}, SkillYouTube); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordStart("C", "Z", "c", SkillYouTube, base.Add(time.Minute), QueuePromote); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordStart("B", "Y", "b", SkillYouTube, base.Add(2*time.Minute), QueuePromote); err != nil {
		t.Fatal(err)
	}
	plays, err := db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 3 || !plays[1].Current {
		t.Fatalf("prev promote B: %+v", plays)
	}
}

func TestLikeDoesNotWriteHistory(t *testing.T) {
	db := openTemp(t)
	likedAt := time.Date(2026, 11, 8, 4, 0, 0, 0, time.UTC)
	if err := db.Like("Get Lucky", "Daft Punk", "abc", likedAt, likedAt); err != nil {
		t.Fatal(err)
	}
	hist, err := db.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 0 {
		t.Fatalf("history %+v", hist)
	}
	plays, err := db.Plays()
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 0 {
		t.Fatalf("plays %+v", plays)
	}
}

func TestPlaybackRoundTrip(t *testing.T) {
	db := openTemp(t)
	if err := db.SavePlayback(MediaMusic, "Монетка", "ЛСП", "k9", 95025); err != nil {
		t.Fatal(err)
	}
	got, ms, ok, err := db.LoadPlayback(MediaMusic)
	if err != nil || !ok || ms != 95025 || got.Title != "Монетка" {
		t.Fatalf("got %+v ms %d ok %v err %v", got, ms, ok, err)
	}
}
