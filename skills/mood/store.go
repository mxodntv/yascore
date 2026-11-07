package mood

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"yascore/internal/skillstate"
)

const moodFile = "mood"

type moodDoc struct {
	Mood string `json:"mood"`
}

// Store keeps the one active mood as skills/<mood>/data/mood.json.
type Store struct {
	docs *skillstate.Store
}

// OpenStore opens the mood skill directory from the loaded config.
// The file is created on the first save, not when the store is opened.
func OpenStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return nil, fmt.Errorf("mood: database path is empty")
	}
	docs, err := skillstate.Open(path)
	if err != nil {
		return nil, err
	}
	return &Store{docs: docs}, nil
}

// Close finishes the store.
func (s *Store) Close() error {
	if s == nil || s.docs == nil {
		return nil
	}
	return s.docs.Close()
}

// Load returns the saved mood id. The boolean is false when nothing is saved.
func (s *Store) Load(ctx context.Context) (string, bool, error) {
	if s == nil || s.docs == nil {
		return "", false, nil
	}
	raw, ok, err := s.docs.Read(moodFile)
	if err != nil || !ok {
		return "", false, err
	}
	var doc moodDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false, err
	}
	id := strings.TrimSpace(doc.Mood)
	if id == "" {
		return "", false, nil
	}
	return id, true, nil
}

// Save replaces the mood document.
func (s *Store) Save(ctx context.Context, id string) error {
	return s.docs.Write(moodFile, moodDoc{Mood: strings.TrimSpace(id)})
}
