package youtubemusic

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// previousFile is the older music-only database. Rows are copied once.
	previousFile = "musicplayer.db"
	// ServiceYouTube is the playback service name stored with a track.
	ServiceYouTube = "youtube-music"
)

// Track is one song the player can start again by title.
type Track struct {
	VideoID string
	Title   string
	Artist  string
	Service string
}

// Like is one saved track. A second like of the same title, artist, and service
// updates LikedAt and does not insert another row.
type Like struct {
	Track
	LikedAt      time.Time
	LastPlayedAt time.Time
}

// Play is one row of listening history, oldest first.
type Play struct {
	Track
	PlayedAt   time.Time
	Current    bool
	PositionMS int    // -1 when the player has not reported a spot
	Position   string // the player's own position text, when it is not milliseconds
}

// Snapshot is the queue the player navigates: likes, history, and the last track.
// A nil Likes slice leaves the likes table alone. A non-nil slice is the full list.
type Snapshot struct {
	Likes  []Track
	Plays  []Track
	Index  int
	Recent *Track
}

// Store is the youtube-music tables in the shared SQLite file data/yascore.db.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Open opens path and creates the youtubemusic_ tables.
// An empty path is an error. A sibling musicplayer.db is copied once
// when it has rows and these tables are still empty; that file is not deleted.
// Existing JSON likes and queue files under skills/youtube-music/data are
// copied in once when the tables are still empty. Those JSON files are not
// deleted, and place.json is not read.
func Open(path string) (*Store, error) {
	return OpenWithLegacy(path, filepath.Join("skills", "youtube-music", "data"))
}

// OpenWithLegacy is Open with an explicit directory of old likes.json, history.json,
// and queue.json files. Missing files are skipped.
func OpenWithLegacy(path, legacyDir string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("youtubemusic: database path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	db, err := sql.Open("sqlite", "file://"+abs+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.schema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.importPreviousFile(abs); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.importLegacy(legacyDir); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) schema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS youtubemusic_likes (
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
CREATE TABLE IF NOT EXISTS youtubemusic_plays (
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
CREATE TABLE IF NOT EXISTS youtubemusic_recent (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    title TEXT NOT NULL DEFAULT '',
    artist TEXT NOT NULL DEFAULT '',
    title_key TEXT NOT NULL DEFAULT '',
    artist_key TEXT NOT NULL DEFAULT '',
    service TEXT NOT NULL DEFAULT '',
    video_id TEXT NOT NULL DEFAULT '',
    played_at TEXT NOT NULL DEFAULT ''
);
`)
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	// position_ms is the player's millisecond spot. -1 means it was not reported.
	// position keeps a coarser report when the player has no seconds.
	for _, stmt := range []string{
		`ALTER TABLE youtubemusic_recent ADD COLUMN position_ms INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE youtubemusic_recent ADD COLUMN position TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE youtubemusic_plays ADD COLUMN position_ms INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE youtubemusic_plays ADD COLUMN position TEXT NOT NULL DEFAULT ''`,
	} {
		if _, alterErr := s.db.Exec(stmt); alterErr != nil && !strings.Contains(strings.ToLower(alterErr.Error()), "duplicate column") {
			return fmt.Errorf("youtubemusic: %w", alterErr)
		}
	}
	return nil
}

// Like inserts the track or, if that title, artist, and service are already stored,
// refreshes liked_at and last_played_at.
func (s *Store) Like(title, artist, videoID, service string, likedAt, lastPlayed time.Time) error {
	title, artist, videoID, service, err := cleanTrack(title, artist, videoID, service)
	if err != nil {
		return err
	}
	likedAt = wall(likedAt)
	lastPlayed = wall(lastPlayed)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertLike(tx, title, artist, videoID, service, likedAt, lastPlayed, true); err != nil {
		return err
	}
	recent := Track{VideoID: videoID, Title: title, Artist: artist, Service: service}
	if err := writeRecent(tx, &recent, lastPlayed); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	return nil
}

// Unlike removes the track. The boolean is false when no row matched.
func (s *Store) Unlike(title, artist, videoID, service string) (bool, error) {
	title = strings.TrimSpace(title)
	artist = strings.TrimSpace(artist)
	videoID = strings.TrimSpace(videoID)
	service = strings.TrimSpace(service)
	if service == "" {
		return false, fmt.Errorf("youtubemusic: empty service")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`
DELETE FROM youtubemusic_likes
WHERE service = ? AND title_key = ? AND artist_key = ?`,
		service, fold(title), fold(artist))
	if err != nil {
		return false, fmt.Errorf("youtubemusic: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("youtubemusic: %w", err)
	}
	if n == 0 && videoID != "" {
		res, err = s.db.Exec(`DELETE FROM youtubemusic_likes WHERE service = ? AND video_id = ?`, service, videoID)
		if err != nil {
			return false, fmt.Errorf("youtubemusic: %w", err)
		}
		n, err = res.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("youtubemusic: %w", err)
		}
	}
	return n > 0, nil
}

// Likes returns saved tracks, oldest first.
func (s *Store) Likes() ([]Like, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT title, artist, service, liked_at, last_played_at, video_id
FROM youtubemusic_likes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	defer rows.Close()
	var out []Like
	for rows.Next() {
		var item Like
		var liked, played string
		if err := rows.Scan(&item.Title, &item.Artist, &item.Service, &liked, &played, &item.VideoID); err != nil {
			return nil, fmt.Errorf("youtubemusic: %w", err)
		}
		item.LikedAt, err = parseTime(liked)
		if err != nil {
			return nil, err
		}
		item.LastPlayedAt, err = parseTime(played)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	if out == nil {
		out = []Like{}
	}
	return out, nil
}

// AppendPlay adds one history row and makes it current. Rows are not capped at 20.
func (s *Store) AppendPlay(title, artist, videoID, service string, at time.Time) error {
	title, artist, videoID, service, err := cleanTrack(title, artist, videoID, service)
	if err != nil {
		return err
	}
	at = wall(at)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var max sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(seq) FROM youtubemusic_plays`).Scan(&max); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	seq := 0
	if max.Valid {
		seq = int(max.Int64) + 1
	}
	if _, err := tx.Exec(`UPDATE youtubemusic_plays SET current = 0`); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	if _, err := tx.Exec(`
INSERT INTO youtubemusic_plays (seq, title, artist, title_key, artist_key, service, played_at, video_id, current)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		seq, title, artist, fold(title), fold(artist), service, formatTime(at), videoID); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	recent := Track{VideoID: videoID, Title: title, Artist: artist, Service: service}
	if err := writeRecent(tx, &recent, at); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	return nil
}

// Plays returns history, oldest first.
func (s *Store) Plays() ([]Play, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readPlays()
}

// PreviousTitle is the play before the current one.
func (s *Store) PreviousTitle() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var seq int
	err := s.db.QueryRow(`SELECT seq FROM youtubemusic_plays WHERE current = 1 LIMIT 1`).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("youtubemusic: %w", err)
	}
	var title string
	err = s.db.QueryRow(`SELECT title FROM youtubemusic_plays WHERE seq = ?`, seq-1).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("youtubemusic: %w", err)
	}
	return title, true, nil
}

// LastPlayed is when this track was last written into history or the recent pointer.
func (s *Store) LastPlayed(title, artist, service string) (time.Time, bool, error) {
	service = strings.TrimSpace(service)
	titleKey, artistKey := fold(title), fold(artist)
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	err := s.db.QueryRow(`
SELECT played_at FROM youtubemusic_plays
WHERE service = ? AND title_key = ? AND artist_key = ?
ORDER BY played_at DESC, seq DESC LIMIT 1`, service, titleKey, artistKey).Scan(&raw)
	if err == nil {
		t, parseErr := parseTime(raw)
		return t, true, parseErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, fmt.Errorf("youtubemusic: %w", err)
	}
	var recentService, recentTitle, recentArtist, played string
	err = s.db.QueryRow(`SELECT service, title_key, artist_key, played_at FROM youtubemusic_recent WHERE id = 1`).Scan(
		&recentService, &recentTitle, &recentArtist, &played)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("youtubemusic: %w", err)
	}
	if recentService != service || recentTitle != titleKey || recentArtist != artistKey || played == "" {
		return time.Time{}, false, nil
	}
	t, err := parseTime(played)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

// Load reads likes, history, the current index, and the last track.
func (s *Store) Load() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	likes, err := s.readLikeTracks()
	if err != nil {
		return Snapshot{}, err
	}
	plays, err := s.readPlays()
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Likes: make([]Track, 0, len(likes)), Plays: make([]Track, 0, len(plays)), Index: 0}
	for _, item := range likes {
		snap.Likes = append(snap.Likes, item.Track)
	}
	for i, item := range plays {
		snap.Plays = append(snap.Plays, item.Track)
		if item.Current {
			snap.Index = i
		}
	}
	recent, ok, err := s.readRecent()
	if err != nil {
		return Snapshot{}, err
	}
	if ok {
		snap.Recent = &recent
	}
	return snap, nil
}

// Save writes the queue and, when Likes is non-nil, makes the likes table match that list.
// Existing likes keep their timestamps. PlayedAt is kept for a track that stays in the same place.
func (s *Store) Save(snap Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.readPlays()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if snap.Likes != nil {
		if err := syncLikes(tx, snap.Likes); err != nil {
			return err
		}
	}
	if err := replacePlays(tx, old, snap.Plays, snap.Index); err != nil {
		return err
	}
	if err := writeRecent(tx, snap.Recent, time.Time{}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	return nil
}

func (s *Store) readLikeTracks() ([]Like, error) {
	rows, err := s.db.Query(`
SELECT title, artist, service, liked_at, last_played_at, video_id
FROM youtubemusic_likes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	defer rows.Close()
	var out []Like
	for rows.Next() {
		var item Like
		var liked, played string
		if err := rows.Scan(&item.Title, &item.Artist, &item.Service, &liked, &played, &item.VideoID); err != nil {
			return nil, fmt.Errorf("youtubemusic: %w", err)
		}
		item.LikedAt, err = parseTime(liked)
		if err != nil {
			return nil, err
		}
		item.LastPlayedAt, err = parseTime(played)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	return out, nil
}

func (s *Store) readPlays() ([]Play, error) {
	rows, err := s.db.Query(`
SELECT title, artist, service, played_at, video_id, current, position_ms, position
FROM youtubemusic_plays ORDER BY seq`)
	if err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	defer rows.Close()
	var out []Play
	for rows.Next() {
		var item Play
		var played string
		var current int
		if err := rows.Scan(&item.Title, &item.Artist, &item.Service, &played, &item.VideoID, &current, &item.PositionMS, &item.Position); err != nil {
			return nil, fmt.Errorf("youtubemusic: %w", err)
		}
		item.PlayedAt, err = parseTime(played)
		if err != nil {
			return nil, err
		}
		item.Current = current == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	return out, nil
}

// Remember writes the playing track and the spot where playback is, or where it was paused.
// The current history row and the recent row both keep that spot so resume can seek.
// positionMS is -1 when the player did not report milliseconds. raw is the best text it did report.
func (s *Store) Remember(track Track, positionMS int, raw string) error {
	if s == nil || s.db == nil {
		return nil
	}
	raw = strings.TrimSpace(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if strings.TrimSpace(track.Title) != "" || strings.TrimSpace(track.VideoID) != "" {
		if err := writeRecent(tx, &track, time.Time{}); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE youtubemusic_recent SET position_ms = ?, position = ? WHERE id = 1`, positionMS, raw); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	if _, err := tx.Exec(`UPDATE youtubemusic_plays SET position_ms = ?, position = ? WHERE current = 1`, positionMS, raw); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	return nil
}

// CurrentPosition is the spot stored on the recent row.
// ok is false when no row has a position. ms is -1 when only a coarser position was stored.
func (s *Store) CurrentPosition() (ms int, raw string, ok bool, err error) {
	if s == nil || s.db == nil {
		return -1, "", false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var title string
	err = s.db.QueryRow(`SELECT title, position_ms, position FROM youtubemusic_recent WHERE id = 1`).Scan(&title, &ms, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, "", false, nil
	}
	if err != nil {
		return -1, "", false, fmt.Errorf("youtubemusic: %w", err)
	}
	if ms < 0 && strings.TrimSpace(raw) == "" {
		return -1, "", false, nil
	}
	return ms, raw, true, nil
}

func (s *Store) readRecent() (Track, bool, error) {
	var track Track
	err := s.db.QueryRow(`SELECT title, artist, service, video_id FROM youtubemusic_recent WHERE id = 1`).Scan(
		&track.Title, &track.Artist, &track.Service, &track.VideoID)
	if errors.Is(err, sql.ErrNoRows) {
		return Track{}, false, nil
	}
	if err != nil {
		return Track{}, false, fmt.Errorf("youtubemusic: %w", err)
	}
	if strings.TrimSpace(track.Title) == "" && strings.TrimSpace(track.VideoID) == "" {
		return Track{}, false, nil
	}
	return track, true, nil
}

func syncLikes(tx *sql.Tx, tracks []Track) error {
	rows, err := tx.Query(`SELECT id, service, title_key, artist_key FROM youtubemusic_likes`)
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	type row struct {
		id       int64
		identity string
	}
	var existing []row
	for rows.Next() {
		var item row
		var service, titleKey, artistKey string
		if err := rows.Scan(&item.id, &service, &titleKey, &artistKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("youtubemusic: %w", err)
		}
		item.identity = likeKey(service, titleKey, artistKey)
		existing = append(existing, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("youtubemusic: %w", err)
	}
	_ = rows.Close()

	want := map[string]Track{}
	for _, track := range tracks {
		title, artist, videoID, service, err := cleanTrack(track.Title, track.Artist, track.VideoID, serviceOrYouTube(track.Service))
		if err != nil {
			continue
		}
		track.Title, track.Artist, track.VideoID, track.Service = title, artist, videoID, service
		want[likeKey(service, fold(title), fold(artist))] = track
	}
	for _, item := range existing {
		if _, ok := want[item.identity]; ok {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM youtubemusic_likes WHERE id = ?`, item.id); err != nil {
			return fmt.Errorf("youtubemusic: %w", err)
		}
	}
	now := wall(time.Now())
	for _, track := range want {
		if err := upsertLike(tx, track.Title, track.Artist, track.VideoID, track.Service, now, now, false); err != nil {
			return err
		}
	}
	return nil
}

func upsertLike(tx *sql.Tx, title, artist, videoID, service string, likedAt, lastPlayed time.Time, refreshTimes bool) error {
	titleKey, artistKey := fold(title), fold(artist)
	if refreshTimes {
		_, err := tx.Exec(`
INSERT INTO youtubemusic_likes (title, artist, title_key, artist_key, service, liked_at, last_played_at, video_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(service, title_key, artist_key) DO UPDATE SET
    liked_at = excluded.liked_at,
    last_played_at = excluded.last_played_at,
    title = excluded.title,
    artist = excluded.artist,
    video_id = CASE WHEN excluded.video_id <> '' THEN excluded.video_id ELSE youtubemusic_likes.video_id END`,
			title, artist, titleKey, artistKey, service, formatTime(likedAt), formatTime(lastPlayed), videoID)
		if err != nil {
			return fmt.Errorf("youtubemusic: %w", err)
		}
		return nil
	}
	_, err := tx.Exec(`
INSERT INTO youtubemusic_likes (title, artist, title_key, artist_key, service, liked_at, last_played_at, video_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(service, title_key, artist_key) DO UPDATE SET
    title = excluded.title,
    artist = excluded.artist,
    video_id = CASE WHEN excluded.video_id <> '' THEN excluded.video_id ELSE youtubemusic_likes.video_id END`,
		title, artist, titleKey, artistKey, service, formatTime(likedAt), formatTime(lastPlayed), videoID)
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	return nil
}

func replacePlays(tx *sql.Tx, old []Play, tracks []Track, index int) error {
	if _, err := tx.Exec(`DELETE FROM youtubemusic_plays`); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	if len(tracks) == 0 {
		return nil
	}
	if index < 0 || index >= len(tracks) {
		index = len(tracks) - 1
	}
	now := wall(time.Now())
	for i, track := range tracks {
		title, artist, videoID, service, err := cleanTrack(track.Title, track.Artist, track.VideoID, serviceOrYouTube(track.Service))
		if err != nil {
			continue
		}
		at := now
		if i < len(old) && samePlace(old[i], title, artist, service) {
			at = old[i].PlayedAt
		}
		current := 0
		if i == index {
			current = 1
		}
		posMS := -1
		posText := ""
		if i < len(old) && samePlace(old[i], title, artist, service) {
			posMS = old[i].PositionMS
			posText = old[i].Position
		}
		if _, err := tx.Exec(`
INSERT INTO youtubemusic_plays (seq, title, artist, title_key, artist_key, service, played_at, video_id, current, position_ms, position)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			i, title, artist, fold(title), fold(artist), service, formatTime(at), videoID, current, posMS, posText); err != nil {
			return fmt.Errorf("youtubemusic: %w", err)
		}
	}
	return nil
}

func writeRecent(tx *sql.Tx, track *Track, played time.Time) error {
	if track == nil || (strings.TrimSpace(track.Title) == "" && strings.TrimSpace(track.VideoID) == "") {
		if _, err := tx.Exec(`DELETE FROM youtubemusic_recent WHERE id = 1`); err != nil {
			return fmt.Errorf("youtubemusic: %w", err)
		}
		return nil
	}
	title, artist, videoID, service, err := cleanTrack(track.Title, track.Artist, track.VideoID, serviceOrYouTube(track.Service))
	if err != nil {
		if _, delErr := tx.Exec(`DELETE FROM youtubemusic_recent WHERE id = 1`); delErr != nil {
			return fmt.Errorf("youtubemusic: %w", delErr)
		}
		return nil
	}
	if played.IsZero() {
		var raw string
		err := tx.QueryRow(`SELECT played_at FROM youtubemusic_recent WHERE id = 1 AND service = ? AND title_key = ? AND artist_key = ?`,
			service, fold(title), fold(artist)).Scan(&raw)
		if err == nil && raw != "" {
			played, err = parseTime(raw)
			if err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("youtubemusic: %w", err)
		}
	}
	if played.IsZero() {
		var raw string
		err := tx.QueryRow(`
SELECT played_at FROM youtubemusic_plays
WHERE service = ? AND title_key = ? AND artist_key = ?
ORDER BY seq DESC LIMIT 1`, service, fold(title), fold(artist)).Scan(&raw)
		if err == nil && raw != "" {
			played, err = parseTime(raw)
			if err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("youtubemusic: %w", err)
		}
	}
	if played.IsZero() {
		played = wall(time.Now())
	}
	_, err = tx.Exec(`
INSERT INTO youtubemusic_recent (id, title, artist, title_key, artist_key, service, video_id, played_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    artist = excluded.artist,
    title_key = excluded.title_key,
    artist_key = excluded.artist_key,
    service = excluded.service,
    video_id = excluded.video_id,
    played_at = excluded.played_at`,
		title, artist, fold(title), fold(artist), service, videoID, formatTime(played))
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	return nil
}

func (s *Store) importPreviousFile(newAbs string) error {
	oldAbs := filepath.Join(filepath.Dir(newAbs), previousFile)
	if filepath.Clean(oldAbs) == filepath.Clean(newAbs) {
		return nil
	}
	if _, err := os.Stat(oldAbs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("youtubemusic: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.rowCount()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	old, err := sql.Open("sqlite", "file://"+oldAbs+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	defer old.Close()
	old.SetMaxOpenConns(1)
	present, err := previousTables(old)
	if err != nil {
		return err
	}
	if len(present) == 0 {
		return nil
	}
	total, err := countNamed(old, present)
	if err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if present["musicplayer_likes"] {
		if err := copyTable(old, tx, "musicplayer_likes", "youtubemusic_likes",
			"id, title, artist, title_key, artist_key, service, liked_at, last_played_at, video_id"); err != nil {
			return err
		}
	}
	if present["musicplayer_plays"] {
		if err := copyTable(old, tx, "musicplayer_plays", "youtubemusic_plays",
			"id, seq, title, artist, title_key, artist_key, service, played_at, video_id, current"); err != nil {
			return err
		}
	}
	if present["musicplayer_recent"] {
		if err := copyTable(old, tx, "musicplayer_recent", "youtubemusic_recent",
			"id, title, artist, title_key, artist_key, service, video_id, played_at"); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	return nil
}

func (s *Store) rowCount() (int, error) {
	var n int
	if err := s.db.QueryRow(`
SELECT
    (SELECT COUNT(*) FROM youtubemusic_likes) +
    (SELECT COUNT(*) FROM youtubemusic_plays) +
    (SELECT COUNT(*) FROM youtubemusic_recent)`).Scan(&n); err != nil {
		return 0, fmt.Errorf("youtubemusic: %w", err)
	}
	return n, nil
}

func previousTables(old *sql.DB) (map[string]bool, error) {
	rows, err := old.Query(`
SELECT name FROM sqlite_master
WHERE type = 'table' AND name IN ('musicplayer_likes', 'musicplayer_plays', 'musicplayer_recent')`)
	if err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("youtubemusic: %w", err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("youtubemusic: %w", err)
	}
	return out, nil
}

func countNamed(old *sql.DB, present map[string]bool) (int, error) {
	total := 0
	for _, name := range []string{"musicplayer_likes", "musicplayer_plays", "musicplayer_recent"} {
		if !present[name] {
			continue
		}
		var n int
		if err := old.QueryRow("SELECT COUNT(*) FROM " + name).Scan(&n); err != nil {
			return 0, fmt.Errorf("youtubemusic: %w", err)
		}
		total += n
	}
	return total, nil
}

func copyTable(old *sql.DB, tx *sql.Tx, from, to, cols string) error {
	rows, err := old.Query("SELECT " + cols + " FROM " + from)
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	defer rows.Close()
	nCols := len(strings.Split(cols, ","))
	placeholders := strings.TrimRight(strings.Repeat("?,", nCols), ",")
	insert := "INSERT INTO " + to + " (" + cols + ") VALUES (" + placeholders + ")"
	for rows.Next() {
		vals := make([]any, nCols)
		ptrs := make([]any, nCols)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("youtubemusic: %w", err)
		}
		if _, err := tx.Exec(insert, vals...); err != nil {
			return fmt.Errorf("youtubemusic: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	return nil
}

func (s *Store) importLegacy(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.rowCount()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	likes, likesOK, err := readLegacyLikes(filepath.Join(dir, "likes.json"))
	if err != nil {
		return err
	}
	plays, index, playsOK, err := readLegacyQueue(filepath.Join(dir, "queue.json"))
	if err != nil {
		return err
	}
	recent, recentOK, err := readLegacyRecent(filepath.Join(dir, "history.json"))
	if err != nil {
		return err
	}
	if !likesOK && !playsOK && !recentOK {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := wall(time.Now())
	for _, track := range likes {
		title, artist, videoID, service, cleanErr := cleanTrack(track.Title, track.Artist, track.VideoID, serviceOrYouTube(track.Service))
		if cleanErr != nil {
			continue
		}
		if err := upsertLike(tx, title, artist, videoID, service, now, now, true); err != nil {
			return err
		}
	}
	if err := replacePlays(tx, nil, plays, index); err != nil {
		return err
	}
	if recentOK {
		if err := writeRecent(tx, &recent, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("youtubemusic: %w", err)
	}
	return nil
}

type legacyTrack struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Artists string `json:"artists"`
}

func readLegacyLikes(path string) ([]Track, bool, error) {
	var doc struct {
		Likes []legacyTrack `json:"likes"`
	}
	ok, err := readLegacy(path, &doc)
	if err != nil || !ok {
		return nil, ok, err
	}
	return legacyTracks(doc.Likes), true, nil
}

func readLegacyQueue(path string) ([]Track, int, bool, error) {
	var doc struct {
		Tracks []legacyTrack `json:"tracks"`
		Index  int           `json:"index"`
	}
	ok, err := readLegacy(path, &doc)
	if err != nil || !ok {
		return nil, 0, ok, err
	}
	return legacyTracks(doc.Tracks), doc.Index, true, nil
}

func readLegacyRecent(path string) (Track, bool, error) {
	var doc struct {
		Recent *legacyTrack `json:"recent"`
	}
	ok, err := readLegacy(path, &doc)
	if err != nil || !ok || doc.Recent == nil {
		return Track{}, false, err
	}
	track := Track{VideoID: doc.Recent.ID, Title: doc.Recent.Title, Artist: doc.Recent.Artists, Service: ServiceYouTube}
	if strings.TrimSpace(track.Title) == "" && strings.TrimSpace(track.VideoID) == "" {
		return Track{}, false, nil
	}
	return track, true, nil
}

func readLegacy(path string, dest any) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("youtubemusic: %w", err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return false, nil
	}
	return true, nil
}

func legacyTracks(list []legacyTrack) []Track {
	out := make([]Track, 0, len(list))
	for _, item := range list {
		out = append(out, Track{
			VideoID: item.ID,
			Title:   item.Title,
			Artist:  item.Artists,
			Service: ServiceYouTube,
		})
	}
	return out
}

func cleanTrack(title, artist, videoID, service string) (string, string, string, string, error) {
	title = strings.TrimSpace(title)
	artist = strings.TrimSpace(artist)
	videoID = strings.TrimSpace(videoID)
	service = strings.TrimSpace(service)
	if service == "" {
		return "", "", "", "", fmt.Errorf("youtubemusic: empty service")
	}
	if title == "" && videoID == "" {
		return "", "", "", "", fmt.Errorf("youtubemusic: empty track")
	}
	return title, artist, videoID, service, nil
}

func serviceOrYouTube(service string) string {
	service = strings.TrimSpace(service)
	if service == "" {
		return ServiceYouTube
	}
	return service
}

func samePlace(old Play, title, artist, service string) bool {
	return old.Service == service && fold(old.Title) == fold(title) && fold(old.Artist) == fold(artist)
}

func likeKey(service, titleKey, artistKey string) string {
	return service + "\x00" + titleKey + "\x00" + artistKey
}

func fold(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func wall(t time.Time) time.Time {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Truncate(time.Second)
}

func formatTime(t time.Time) string {
	return wall(t).Format(time.RFC3339)
}

func parseTime(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("youtubemusic: time %q: %w", raw, err)
	}
	return t.UTC(), nil
}
