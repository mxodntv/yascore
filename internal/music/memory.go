package music

import "time"

// PlayerMemory adapts Store to the column player's pause and history hooks.
type PlayerMemory struct {
	Store *Store
}

func (m PlayerMemory) SavePlayback(media, title, artist, externalID string, positionMS int) error {
	if m.Store == nil {
		return nil
	}
	return m.Store.SavePlayback(media, title, artist, externalID, positionMS)
}

func (m PlayerMemory) LoadPlayback(media string) (title, artist, externalID string, positionMS int, ok bool, err error) {
	if m.Store == nil {
		return "", "", "", -1, false, nil
	}
	track, ms, ok, err := m.Store.LoadPlayback(media)
	if err != nil || !ok {
		return "", "", "", -1, ok, err
	}
	return track.Title, track.Artist, track.ExternalID, ms, true, nil
}

func (m PlayerMemory) ClearPlayback(media string) error {
	if m.Store == nil {
		return nil
	}
	return m.Store.ClearPlayback(media)
}

func (m PlayerMemory) RandomLike() (title, artist, externalID string, ok bool, err error) {
	if m.Store == nil {
		return "", "", "", false, nil
	}
	track, ok, err := m.Store.RandomLike()
	if err != nil || !ok {
		return "", "", "", ok, err
	}
	return track.Title, track.Artist, track.ExternalID, true, nil
}

func (m PlayerMemory) RecordStart(title, artist, externalID, skill string, at time.Time, appendQueue bool) error {
	if m.Store == nil {
		return nil
	}
	queue := QueueAuto
	if appendQueue {
		// next/prev/auto-switch: promote EnsureQueued row when present.
		queue = QueuePromote
	}
	return m.Store.RecordStart(title, artist, externalID, skill, at, queue)
}

func (m PlayerMemory) FinishListen(listenedMS int) error {
	if m.Store == nil {
		return nil
	}
	return m.Store.FinishListen(listenedMS)
}

// EnsureQueued writes upcoming plays with current=0. Each item is title, artist, externalID.
func (m PlayerMemory) EnsureQueued(titles, artists, externalIDs []string, skill string) error {
	if m.Store == nil {
		return nil
	}
	n := len(titles)
	if len(artists) < n {
		n = len(artists)
	}
	if len(externalIDs) < n {
		n = len(externalIDs)
	}
	tracks := make([]Track, 0, n)
	for i := 0; i < n; i++ {
		tracks = append(tracks, Track{Title: titles[i], Artist: artists[i], ExternalID: externalIDs[i]})
	}
	return m.Store.EnsureQueued(tracks, skill)
}
