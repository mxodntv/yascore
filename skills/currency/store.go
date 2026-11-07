package currency

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"yascore/internal/skillstate"
)

const currencyFile = "default"

type currencyDoc struct {
	Code string `json:"code"`
}

// Store keeps the default currency as the currency document.
type Store struct {
	docs *skillstate.Store
}

// OpenStore opens the currency skill directory.
// An empty path uses skills/currency. The saved code is data/default.json.
// data/skills.json is the old shared file and is not opened for new writes.
func OpenStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return nil, fmt.Errorf("currency: database path is empty")
	}
	if strings.EqualFold(filepath.Ext(path), ".db") {
		path = filepath.Dir(path)
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

// Load returns the saved currency code. The boolean is false when nothing is saved.
func (s *Store) Load(ctx context.Context) (string, bool, error) {
	if s == nil || s.docs == nil {
		return "", false, nil
	}
	raw, ok, err := s.docs.Read(currencyFile)
	if err != nil || !ok {
		return "", false, err
	}
	var doc currencyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false, err
	}
	code := strings.ToUpper(strings.TrimSpace(doc.Code))
	if code == "" {
		return "", false, nil
	}
	return code, true, nil
}

// Save replaces the currency document.
func (s *Store) Save(ctx context.Context, code string) error {
	return s.docs.Write(currencyFile, currencyDoc{Code: strings.ToUpper(strings.TrimSpace(code))})
}
