// Package music is the shared likes, play history, and pause store for every
// music backend (YouTube Music now; others later). Cards are keyed by title+artist.
package music

import (
	"database/sql"
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
	// MediaMusic is the playback row for audio.
	MediaMusic = "music"
	// SkillYouTube is the musicyoutube backend name stored on play events.
	SkillYouTube = "musicyoutube"
)

// Track is one song card shared across platforms.
type Track struct {
	ID         int64
	Title      string
	Artist     string
	ExternalID string // last known id on a platform; not the like identity
}

// Like is a liked track card.
type Like struct {
	Track
	LikedAt      time.Time
	LastPlayedAt time.Time
	PlayCount    int
}

// Play is one queue row. Skill and ExternalID name where it played.
type Play struct {
	Track
	Skill      string
	ExternalID string
	PlayedAt   time.Time
	Current    bool
}

// HistoryEntry is one listening session in the append-only journal.
type HistoryEntry struct {
	ID         int64
	Track
	Skill      string
	StartedAt  time.Time
	ListenedMS int
}

// QueuePolicy controls whether RecordStart appends or promotes a music_plays row.
type QueuePolicy int

const (
	// QueueAuto appends a play only when the current queue row is a different track.
	QueueAuto QueuePolicy = iota
	// QueueAppend always appends a music_plays row (new search/play, mix).
	QueueAppend
	// QueuePromote moves current to an existing queued play for this track (next/prev),
	// or appends when the track is not in music_plays yet.
	QueuePromote
)

// Snapshot is likes, history queue, and the recent track.
type Snapshot struct {
	Likes  []Track
	Plays  []Track
	Index  int
	Recent *Track
}

// Store is music_* tables in data/yascore.db.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Open opens path and creates music_* tables. No legacy import.
func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("music: database path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("music: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("music: %w", err)
	}
	db, err := sql.Open("sqlite", "file://"+abs+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("music: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.schema(); err != nil {
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
CREATE TABLE IF NOT EXISTS music_tracks (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    artist TEXT NOT NULL DEFAULT '',
    title_key TEXT NOT NULL,
    artist_key TEXT NOT NULL,
    liked_at TEXT NOT NULL DEFAULT '',
    last_played_at TEXT NOT NULL DEFAULT '',
    play_count INTEGER NOT NULL DEFAULT 0,
    external_id TEXT NOT NULL DEFAULT '',
    UNIQUE (title_key, artist_key)
);
CREATE TABLE IF NOT EXISTS music_plays (
    id INTEGER PRIMARY KEY,
    track_id INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    played_at TEXT NOT NULL,
    current INTEGER NOT NULL DEFAULT 0,
    skill TEXT NOT NULL DEFAULT '',
    external_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS music_history (
    id INTEGER PRIMARY KEY,
    track_id INTEGER NOT NULL,
    started_at TEXT NOT NULL,
    listened_ms INTEGER NOT NULL DEFAULT 0,
    skill TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS music_playback (
    media TEXT PRIMARY KEY,
    track_id INTEGER,
    position_ms INTEGER NOT NULL DEFAULT -1
);
`)
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}
	return nil
}

// Like marks the card liked. A repeat like always refreshes liked_at.
func (s *Store) Like(title, artist, externalID string, likedAt, lastPlayed time.Time) error {
	title, artist, externalID, err := cleanTrack(title, artist, externalID)
	if err != nil {
		return err
	}
	likedAt = wall(likedAt)
	lastPlayed = wall(lastPlayed)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	id, err := upsertTrack(tx, title, artist, externalID)
	if err != nil {
		return err
	}
	var existingPlayed string
	if err := tx.QueryRow(`SELECT last_played_at FROM music_tracks WHERE id = ?`, id).Scan(&existingPlayed); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	played := existingPlayed
	if strings.TrimSpace(played) == "" {
		played = formatTime(lastPlayed)
	}
	if _, err := tx.Exec(`UPDATE music_tracks SET liked_at = ?, last_played_at = ? WHERE id = ?`, formatTime(likedAt), played, id); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	if err := writeRecentTrack(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Unlike clears liked_at.
func (s *Store) Unlike(title, artist, externalID string) (bool, error) {
	title = strings.TrimSpace(title)
	artist = strings.TrimSpace(artist)
	externalID = strings.TrimSpace(externalID)
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`
UPDATE music_tracks SET liked_at = ''
WHERE title_key = ? AND artist_key = ? AND liked_at <> ''`, fold(title), foldArtist(artist))
	if err != nil {
		return false, fmt.Errorf("music: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("music: %w", err)
	}
	if n == 0 && externalID != "" {
		res, err = s.db.Exec(`UPDATE music_tracks SET liked_at = '' WHERE external_id = ? AND liked_at <> ''`, externalID)
		if err != nil {
			return false, fmt.Errorf("music: %w", err)
		}
		n, err = res.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("music: %w", err)
		}
	}
	return n > 0, nil
}

// Likes returns liked cards, oldest first.
func (s *Store) Likes() ([]Like, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLikes()
}

// AppendPlay records a new queue tail and a history row (QueueAppend).
func (s *Store) AppendPlay(title, artist, externalID, skill string, at time.Time) error {
	return s.RecordStart(title, artist, externalID, skill, at, QueueAppend)
}

// RecordStart upserts the card, bumps play_count, appends music_history, and
// updates the plays queue according to policy.
func (s *Store) RecordStart(title, artist, externalID, skill string, at time.Time, queue QueuePolicy) error {
	title, artist, externalID, err := cleanTrack(title, artist, externalID)
	if err != nil {
		return err
	}
	skill = strings.TrimSpace(skill)
	at = wall(at)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	id, err := upsertTrack(tx, title, artist, externalID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
UPDATE music_tracks
SET play_count = play_count + 1, last_played_at = ?,
    external_id = CASE WHEN ? <> '' THEN ? ELSE external_id END
WHERE id = ?`, formatTime(at), externalID, externalID, id); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	if _, err := tx.Exec(`
INSERT INTO music_history (track_id, started_at, listened_ms, skill)
VALUES (?, ?, 0, ?)`, id, formatTime(at), skill); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	switch queue {
	case QueuePromote:
		if err := promoteOrAppendPlay(tx, id, skill, externalID, at); err != nil {
			return err
		}
	case QueueAppend:
		if err := appendPlayRow(tx, id, skill, externalID, at); err != nil {
			return err
		}
	case QueueAuto:
		same, err := currentPlayIs(tx, id)
		if err != nil {
			return err
		}
		if !same {
			if err := appendPlayRow(tx, id, skill, externalID, at); err != nil {
				return err
			}
		}
	}
	if err := writeRecentTrack(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// EnsureQueued appends upcoming tracks after the current play with current=0.
// Does not bump play_count or write history. Skips tracks already in the tail.
func (s *Store) EnsureQueued(items []Track, skill string) error {
	if s == nil || s.db == nil || len(items) == 0 {
		return nil
	}
	skill = strings.TrimSpace(skill)
	at := wall(time.Now())
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var curSeq sql.NullInt64
	_ = tx.QueryRow(`SELECT seq FROM music_plays WHERE current = 1 LIMIT 1`).Scan(&curSeq)
	var max sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(seq) FROM music_plays`).Scan(&max); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	nextSeq := 0
	if max.Valid {
		nextSeq = int(max.Int64) + 1
	}
	// Existing tail track ids after current (or all if no current).
	tail := map[int64]bool{}
	q := `SELECT track_id FROM music_plays`
	args := []any{}
	if curSeq.Valid {
		q += ` WHERE seq > ?`
		args = append(args, curSeq.Int64)
	}
	rows, err := tx.Query(q, args...)
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}
	for rows.Next() {
		var tid int64
		if err := rows.Scan(&tid); err != nil {
			_ = rows.Close()
			return fmt.Errorf("music: %w", err)
		}
		tail[tid] = true
	}
	_ = rows.Close()
	for _, item := range items {
		title, artist, externalID, cleanErr := cleanTrack(item.Title, item.Artist, item.ExternalID)
		if cleanErr != nil {
			continue
		}
		id, err := upsertTrack(tx, title, artist, externalID)
		if err != nil {
			return err
		}
		if tail[id] {
			continue
		}
		if _, err := tx.Exec(`
INSERT INTO music_plays (track_id, seq, played_at, current, skill, external_id)
VALUES (?, ?, ?, 0, ?, ?)`, id, nextSeq, formatTime(at), skill, externalID); err != nil {
			return fmt.Errorf("music: %w", err)
		}
		tail[id] = true
		nextSeq++
	}
	return tx.Commit()
}

// FinishListen sets listened_ms on the latest open history row (listened_ms=0).
func (s *Store) FinishListen(listenedMS int) error {
	if s == nil || s.db == nil {
		return nil
	}
	if listenedMS < 0 {
		listenedMS = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`
UPDATE music_history
SET listened_ms = ?
WHERE id = (
    SELECT id FROM music_history WHERE listened_ms = 0 ORDER BY id DESC LIMIT 1
)`, listenedMS)
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}
	_, err = res.RowsAffected()
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}
	return nil
}

// History returns the listening journal, oldest first.
func (s *Store) History() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readHistory()
}

// Plays returns the navigation queue, oldest first.
func (s *Store) Plays() ([]Play, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readPlays()
}

// PreviousTitle is the title before the current play.
func (s *Store) PreviousTitle() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var seq int
	err := s.db.QueryRow(`SELECT seq FROM music_plays WHERE current = 1 LIMIT 1`).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("music: %w", err)
	}
	var title string
	err = s.db.QueryRow(`
SELECT t.title FROM music_plays p
JOIN music_tracks t ON t.id = p.track_id
WHERE p.seq = ?`, seq-1).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("music: %w", err)
	}
	return title, true, nil
}

// LastPlayed is when this card was last played.
func (s *Store) LastPlayed(title, artist string) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	err := s.db.QueryRow(`
SELECT last_played_at FROM music_tracks
WHERE title_key = ? AND artist_key = ? AND last_played_at <> ''`,
		fold(title), foldArtist(artist)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("music: %w", err)
	}
	t, err := parseTime(raw)
	return t, true, err
}

// Load reads likes, plays, index, recent.
func (s *Store) Load() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	likes, err := s.readLikes()
	if err != nil {
		return Snapshot{}, err
	}
	plays, err := s.readPlays()
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Likes: make([]Track, 0, len(likes)), Plays: make([]Track, 0, len(plays))}
	for _, item := range likes {
		snap.Likes = append(snap.Likes, item.Track)
	}
	for i, item := range plays {
		snap.Plays = append(snap.Plays, item.Track)
		if item.Current {
			snap.Index = i
		}
	}
	if len(plays) > 0 {
		recent := plays[len(plays)-1].Track
		for _, item := range plays {
			if item.Current {
				recent = item.Track
				break
			}
		}
		snap.Recent = &recent
	} else if track, _, ok, err := s.readPlaybackTrack(MediaMusic); err != nil {
		return Snapshot{}, err
	} else if ok {
		snap.Recent = &track
	}
	return snap, nil
}

// Save writes the queue and optional full likes list.
func (s *Store) Save(snap Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.readPlays()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("music: %w", err)
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
	if snap.Recent != nil {
		title, artist, externalID, cleanErr := cleanTrack(snap.Recent.Title, snap.Recent.Artist, snap.Recent.ExternalID)
		if cleanErr == nil {
			id, upsertErr := upsertTrack(tx, title, artist, externalID)
			if upsertErr != nil {
				return upsertErr
			}
			if err := writeRecentTrack(tx, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// SavePlayback stores a user pause.
func (s *Store) SavePlayback(media, title, artist, externalID string, positionMS int) error {
	media = strings.TrimSpace(media)
	if media == "" {
		return fmt.Errorf("music: empty media")
	}
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var trackID sql.NullInt64
	if strings.TrimSpace(title) != "" || strings.TrimSpace(externalID) != "" {
		t, a, id, cleanErr := cleanTrack(title, artist, externalID)
		if cleanErr == nil {
			rowID, upsertErr := upsertTrack(tx, t, a, id)
			if upsertErr != nil {
				return upsertErr
			}
			trackID = sql.NullInt64{Int64: rowID, Valid: true}
		}
	}
	if _, err := tx.Exec(`
INSERT INTO music_playback (media, track_id, position_ms)
VALUES (?, ?, ?)
ON CONFLICT(media) DO UPDATE SET
    track_id = excluded.track_id,
    position_ms = excluded.position_ms`, media, trackID, positionMS); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	return tx.Commit()
}

// LoadPlayback returns the stored pause.
func (s *Store) LoadPlayback(media string) (Track, int, bool, error) {
	if s == nil || s.db == nil {
		return Track{}, -1, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readPlaybackTrack(strings.TrimSpace(media))
}

// ClearPlayback removes the pause row.
func (s *Store) ClearPlayback(media string) error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM music_playback WHERE media = ?`, strings.TrimSpace(media)); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	return nil
}

// RandomLike returns one liked card.
func (s *Store) RandomLike() (Track, bool, error) {
	likes, err := s.Likes()
	if err != nil {
		return Track{}, false, err
	}
	if len(likes) == 0 {
		return Track{}, false, nil
	}
	i := int(time.Now().UnixNano() % int64(len(likes)))
	if i < 0 {
		i = -i
	}
	return likes[i].Track, true, nil
}

func (s *Store) readLikes() ([]Like, error) {
	rows, err := s.db.Query(`
SELECT id, title, artist, liked_at, last_played_at, external_id, play_count
FROM music_tracks
WHERE liked_at <> ''
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("music: %w", err)
	}
	defer rows.Close()
	var out []Like
	for rows.Next() {
		var item Like
		var liked, played string
		if err := rows.Scan(&item.ID, &item.Title, &item.Artist, &liked, &played, &item.ExternalID, &item.PlayCount); err != nil {
			return nil, fmt.Errorf("music: %w", err)
		}
		item.LikedAt, err = parseTime(liked)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(played) != "" {
			item.LastPlayedAt, err = parseTime(played)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("music: %w", err)
	}
	if out == nil {
		out = []Like{}
	}
	return out, nil
}

func (s *Store) readPlays() ([]Play, error) {
	rows, err := s.db.Query(`
SELECT t.id, t.title, t.artist, t.external_id, p.skill, p.external_id, p.played_at, p.current
FROM music_plays p
JOIN music_tracks t ON t.id = p.track_id
ORDER BY p.seq`)
	if err != nil {
		return nil, fmt.Errorf("music: %w", err)
	}
	defer rows.Close()
	var out []Play
	for rows.Next() {
		var item Play
		var played string
		var current int
		var trackExt string
		if err := rows.Scan(&item.ID, &item.Title, &item.Artist, &trackExt, &item.Skill, &item.ExternalID, &played, &current); err != nil {
			return nil, fmt.Errorf("music: %w", err)
		}
		if item.ExternalID == "" {
			item.ExternalID = trackExt
		}
		item.Track.ExternalID = item.ExternalID
		item.PlayedAt, err = parseTime(played)
		if err != nil {
			return nil, err
		}
		item.Current = current == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("music: %w", err)
	}
	return out, nil
}

func (s *Store) readHistory() ([]HistoryEntry, error) {
	rows, err := s.db.Query(`
SELECT h.id, t.id, t.title, t.artist, t.external_id, h.skill, h.started_at, h.listened_ms
FROM music_history h
JOIN music_tracks t ON t.id = h.track_id
ORDER BY h.id`)
	if err != nil {
		return nil, fmt.Errorf("music: %w", err)
	}
	defer rows.Close()
	var out []HistoryEntry
	for rows.Next() {
		var item HistoryEntry
		var started string
		if err := rows.Scan(&item.ID, &item.Track.ID, &item.Title, &item.Artist, &item.ExternalID, &item.Skill, &started, &item.ListenedMS); err != nil {
			return nil, fmt.Errorf("music: %w", err)
		}
		item.StartedAt, err = parseTime(started)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("music: %w", err)
	}
	if out == nil {
		out = []HistoryEntry{}
	}
	return out, nil
}

func currentPlayIs(tx *sql.Tx, trackID int64) (bool, error) {
	var id sql.NullInt64
	err := tx.QueryRow(`SELECT track_id FROM music_plays WHERE current = 1 LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("music: %w", err)
	}
	return id.Valid && id.Int64 == trackID, nil
}

// promoteOrAppendPlay moves current onto an existing play for trackID when possible.
func promoteOrAppendPlay(tx *sql.Tx, trackID int64, skill, externalID string, at time.Time) error {
	same, err := currentPlayIs(tx, trackID)
	if err != nil {
		return err
	}
	if same {
		return nil
	}
	var curSeq sql.NullInt64
	_ = tx.QueryRow(`SELECT seq FROM music_plays WHERE current = 1 LIMIT 1`).Scan(&curSeq)

	var playID int64
	found := false
	try := func(query string, args ...any) error {
		err := tx.QueryRow(query, args...).Scan(&playID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("music: %w", err)
		}
		found = true
		return nil
	}
	if curSeq.Valid {
		if err := try(`SELECT id FROM music_plays WHERE track_id = ? AND seq > ? ORDER BY seq ASC LIMIT 1`, trackID, curSeq.Int64); err != nil {
			return err
		}
		if !found {
			if err := try(`SELECT id FROM music_plays WHERE track_id = ? AND seq < ? ORDER BY seq DESC LIMIT 1`, trackID, curSeq.Int64); err != nil {
				return err
			}
		}
	}
	if !found {
		if err := try(`SELECT id FROM music_plays WHERE track_id = ? ORDER BY seq ASC LIMIT 1`, trackID); err != nil {
			return err
		}
	}
	if !found {
		return appendPlayRow(tx, trackID, skill, externalID, at)
	}
	if _, err := tx.Exec(`UPDATE music_plays SET current = 0`); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	if _, err := tx.Exec(`
UPDATE music_plays
SET current = 1, played_at = ?, skill = ?, external_id = CASE WHEN ? <> '' THEN ? ELSE external_id END
WHERE id = ?`, formatTime(at), skill, externalID, externalID, playID); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	return nil
}

func appendPlayRow(tx *sql.Tx, trackID int64, skill, externalID string, at time.Time) error {
	var max sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(seq) FROM music_plays`).Scan(&max); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	seq := 0
	if max.Valid {
		seq = int(max.Int64) + 1
	}
	if _, err := tx.Exec(`UPDATE music_plays SET current = 0`); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	if _, err := tx.Exec(`
INSERT INTO music_plays (track_id, seq, played_at, current, skill, external_id)
VALUES (?, ?, ?, 1, ?, ?)`, trackID, seq, formatTime(at), skill, externalID); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	return nil
}

func (s *Store) readPlaybackTrack(media string) (Track, int, bool, error) {
	var trackID sql.NullInt64
	var ms int
	err := s.db.QueryRow(`SELECT track_id, position_ms FROM music_playback WHERE media = ?`, media).Scan(&trackID, &ms)
	if errors.Is(err, sql.ErrNoRows) {
		return Track{}, -1, false, nil
	}
	if err != nil {
		return Track{}, -1, false, fmt.Errorf("music: %w", err)
	}
	if !trackID.Valid {
		return Track{}, ms, ms >= 0, nil
	}
	var track Track
	err = s.db.QueryRow(`SELECT id, title, artist, external_id FROM music_tracks WHERE id = ?`, trackID.Int64).Scan(
		&track.ID, &track.Title, &track.Artist, &track.ExternalID)
	if errors.Is(err, sql.ErrNoRows) {
		return Track{}, ms, ms >= 0, nil
	}
	if err != nil {
		return Track{}, -1, false, fmt.Errorf("music: %w", err)
	}
	return track, ms, true, nil
}

func syncLikes(tx *sql.Tx, tracks []Track) error {
	rows, err := tx.Query(`SELECT id, title_key, artist_key FROM music_tracks WHERE liked_at <> ''`)
	if err != nil {
		return fmt.Errorf("music: %w", err)
	}
	type row struct {
		id       int64
		identity string
	}
	var existing []row
	for rows.Next() {
		var item row
		var titleKey, artistKey string
		if err := rows.Scan(&item.id, &titleKey, &artistKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("music: %w", err)
		}
		item.identity = titleKey + "\x00" + artistKey
		existing = append(existing, item)
	}
	_ = rows.Close()
	want := map[string]Track{}
	for _, track := range tracks {
		title, artist, externalID, err := cleanTrack(track.Title, track.Artist, track.ExternalID)
		if err != nil {
			continue
		}
		track.Title, track.Artist, track.ExternalID = title, artist, externalID
		want[fold(title)+"\x00"+foldArtist(artist)] = track
	}
	for _, item := range existing {
		if _, ok := want[item.identity]; ok {
			continue
		}
		if _, err := tx.Exec(`UPDATE music_tracks SET liked_at = '' WHERE id = ?`, item.id); err != nil {
			return fmt.Errorf("music: %w", err)
		}
	}
	now := wall(time.Now())
	for _, track := range want {
		id, err := upsertTrack(tx, track.Title, track.Artist, track.ExternalID)
		if err != nil {
			return err
		}
		var liked string
		if err := tx.QueryRow(`SELECT liked_at FROM music_tracks WHERE id = ?`, id).Scan(&liked); err != nil {
			return fmt.Errorf("music: %w", err)
		}
		if strings.TrimSpace(liked) == "" {
			if _, err := tx.Exec(`UPDATE music_tracks SET liked_at = ?, last_played_at = CASE WHEN last_played_at = '' THEN ? ELSE last_played_at END WHERE id = ?`,
				formatTime(now), formatTime(now), id); err != nil {
				return fmt.Errorf("music: %w", err)
			}
		}
	}
	return nil
}

func replacePlays(tx *sql.Tx, old []Play, tracks []Track, index int) error {
	if _, err := tx.Exec(`DELETE FROM music_plays`); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	if len(tracks) == 0 {
		return nil
	}
	if index < 0 || index >= len(tracks) {
		index = len(tracks) - 1
	}
	now := wall(time.Now())
	for i, track := range tracks {
		title, artist, externalID, err := cleanTrack(track.Title, track.Artist, track.ExternalID)
		if err != nil {
			continue
		}
		id, err := upsertTrack(tx, title, artist, externalID)
		if err != nil {
			return err
		}
		at := now
		skill := ""
		playExt := externalID
		if i < len(old) && sameCard(old[i], title, artist) {
			at = old[i].PlayedAt
			skill = old[i].Skill
			if old[i].ExternalID != "" {
				playExt = old[i].ExternalID
			}
		}
		current := 0
		if i == index {
			current = 1
		}
		if _, err := tx.Exec(`
INSERT INTO music_plays (track_id, seq, played_at, current, skill, external_id)
VALUES (?, ?, ?, ?, ?, ?)`, id, i, formatTime(at), current, skill, playExt); err != nil {
			return fmt.Errorf("music: %w", err)
		}
	}
	return nil
}

func writeRecentTrack(tx *sql.Tx, trackID int64) error {
	var ms sql.NullInt64
	err := tx.QueryRow(`SELECT position_ms FROM music_playback WHERE media = ?`, MediaMusic).Scan(&ms)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("music: %w", err)
	}
	pos := -1
	if ms.Valid {
		pos = int(ms.Int64)
	}
	if _, err := tx.Exec(`
INSERT INTO music_playback (media, track_id, position_ms)
VALUES (?, ?, ?)
ON CONFLICT(media) DO UPDATE SET track_id = excluded.track_id`, MediaMusic, trackID, pos); err != nil {
		return fmt.Errorf("music: %w", err)
	}
	return nil
}

func upsertTrack(tx *sql.Tx, title, artist, externalID string) (int64, error) {
	titleKey, artistKey := fold(title), foldArtist(artist)
	_, err := tx.Exec(`
INSERT INTO music_tracks (title, artist, title_key, artist_key, external_id)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(title_key, artist_key) DO UPDATE SET
    title = excluded.title,
    artist = excluded.artist,
    external_id = CASE WHEN excluded.external_id <> '' THEN excluded.external_id ELSE music_tracks.external_id END`,
		title, artist, titleKey, artistKey, externalID)
	if err != nil {
		return 0, fmt.Errorf("music: %w", err)
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM music_tracks WHERE title_key = ? AND artist_key = ?`, titleKey, artistKey).Scan(&id); err != nil {
		return 0, fmt.Errorf("music: %w", err)
	}
	return id, nil
}

func cleanTrack(title, artist, externalID string) (string, string, string, error) {
	title = strings.TrimSpace(title)
	artist = normalizeArtist(artist)
	externalID = strings.TrimSpace(externalID)
	if title == "" && externalID == "" {
		return "", "", "", fmt.Errorf("music: empty track")
	}
	return title, artist, externalID, nil
}

func normalizeArtist(artist string) string {
	artist = strings.TrimSpace(artist)
	const suffix = " - topic"
	if len(artist) >= len(suffix) && strings.EqualFold(artist[len(artist)-len(suffix):], suffix) {
		return strings.TrimSpace(artist[:len(artist)-len(suffix)])
	}
	return artist
}

func sameCard(old Play, title, artist string) bool {
	return fold(old.Title) == fold(title) && foldArtist(old.Artist) == foldArtist(artist)
}

func fold(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func foldArtist(value string) string {
	return fold(normalizeArtist(value))
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
		return time.Time{}, fmt.Errorf("music: time %q: %w", raw, err)
	}
	return t.UTC(), nil
}
