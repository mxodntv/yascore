package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"yascore/internal/skillstate"
)

const placeFile = "place"

// placeDoc is the weather skill document.
// AsksDay is the local calendar day of AsksCount, YYYY-MM-DD.
type placeDoc struct {
	Country   string  `json:"country"`
	City      string  `json:"city"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	AsksDay   string  `json:"asks_day,omitempty"`
	AsksCount int     `json:"asks_count,omitempty"`
}

// Store keeps the default place as the weather document.
type Store struct {
	docs *skillstate.Store
}

// OpenStore opens the weather skill directory.
// An empty path uses skills/weather. Documents are JSON files in <dir>/data.
// A path ending in .db is the old database: the place is written to place.json
// beside that file, and the database is only read.
// data/skills.json is the old shared file: the place already lives in the skill directory.
func OpenStore(path string) (*Store, error) {
	skillDir, legacy, err := resolveWeatherDir(path)
	if err != nil {
		return nil, err
	}
	docs, err := skillstate.Open(skillDir)
	if err != nil {
		return nil, err
	}
	if err := docs.ImportLegacyPlace(legacy); err != nil {
		_ = docs.Close()
		return nil, err
	}
	return &Store{docs: docs}, nil
}

func resolveWeatherDir(path string) (skillDir, legacy string, err error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return "", "", fmt.Errorf("weather: database path is empty")
	}
	if strings.EqualFold(filepath.Ext(path), ".db") {
		return filepath.Dir(path), path, nil
	}
	return path, "", nil
}

// Close finishes the store.
func (s *Store) Close() error {
	if s == nil || s.docs == nil {
		return nil
	}
	return s.docs.Close()
}

// Load returns the default place from the weather document.
// The boolean is false when no city is saved.
func (s *Store) Load(ctx context.Context) (Place, bool, error) {
	doc, ok, err := s.loadDoc(ctx)
	if err != nil || !ok || strings.TrimSpace(doc.City) == "" {
		return Place{}, false, err
	}
	return Place{
		Country:   doc.Country,
		City:      doc.City,
		Latitude:  doc.Latitude,
		Longitude: doc.Longitude,
	}, true, nil
}

// Save writes the default place and keeps the day's ask counter.
func (s *Store) Save(ctx context.Context, p Place) error {
	doc, _, err := s.loadDoc(ctx)
	if err != nil {
		return err
	}
	doc.Country = p.Country
	doc.City = p.City
	doc.Latitude = p.Latitude
	doc.Longitude = p.Longitude
	return s.putDoc(ctx, doc)
}

// noteAsk adds one successful weather ask on day (local YYYY-MM-DD).
// A different day starts again at 1. The saved place is left as it was.
func (s *Store) noteAsk(ctx context.Context, day string) (int, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		return 0, fmt.Errorf("weather: empty day")
	}
	doc, _, err := s.loadDoc(ctx)
	if err != nil {
		return 0, err
	}
	if doc.AsksDay != day {
		doc.AsksDay = day
		doc.AsksCount = 0
	}
	doc.AsksCount++
	if err := s.putDoc(ctx, doc); err != nil {
		return 0, err
	}
	return doc.AsksCount, nil
}

func (s *Store) loadDoc(ctx context.Context) (placeDoc, bool, error) {
	if s == nil || s.docs == nil {
		return placeDoc{}, false, nil
	}
	raw, ok, err := s.docs.Read(placeFile)
	if err != nil || !ok {
		return placeDoc{}, false, err
	}
	var doc placeDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return placeDoc{}, false, err
	}
	return doc, true, nil
}

func (s *Store) putDoc(ctx context.Context, doc placeDoc) error {
	if s == nil || s.docs == nil {
		return fmt.Errorf("weather: store is closed")
	}
	return s.docs.Write(placeFile, doc)
}
