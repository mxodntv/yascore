package skillstate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestWriteReadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	s := openStore(t, dir)
	doc := `{"country":"Россия","city":"Киров","latitude":58.59809,"longitude":49.65783}`
	if err := s.Write("place", doc); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Read("place")
	if err != nil || !ok {
		t.Fatalf("read ok=%v err=%v", ok, err)
	}
	if string(got) != doc {
		t.Fatalf("got %s", got)
	}
	if err := s.Write("other", `{"ok":true}`); err != nil {
		t.Fatal(err)
	}
	other, ok, err := s.Read("other")
	if err != nil || !ok || string(other) != `{"ok":true}` {
		t.Fatalf("other %s ok=%v err=%v", other, ok, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "data", "place.json"))
	if err != nil {
		t.Fatal(err)
	}
	var place map[string]any
	if err := json.Unmarshal(raw, &place); err != nil {
		t.Fatal(err)
	}
	if place["city"] != "Киров" {
		t.Fatalf("file %s", raw)
	}
	otherRaw, err := os.ReadFile(filepath.Join(dir, "data", "other.json"))
	if err != nil {
		t.Fatal(err)
	}
	var extra map[string]any
	if err := json.Unmarshal(otherRaw, &extra); err != nil {
		t.Fatal(err)
	}
	if extra["ok"] != true {
		t.Fatalf("other file %s", otherRaw)
	}
}

func TestSecondWriteReplaces(t *testing.T) {
	s := openStore(t, t.TempDir())
	if err := s.Write("place", `{"country":"Россия","city":"Киров","latitude":1,"longitude":2}`); err != nil {
		t.Fatal(err)
	}
	if err := s.Write("place", `{"city":"Казань"}`); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Read("place")
	if err != nil || !ok {
		t.Fatalf("read ok=%v err=%v", ok, err)
	}
	if string(got) != `{"city":"Казань"}` {
		t.Fatalf("replaced with %s", got)
	}
}

func TestInvalidJSONRejected(t *testing.T) {
	s := openStore(t, t.TempDir())
	if err := s.Write("place", `{"city":"Киров"}`); err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{"", "{", "[]", "null", `"Киров"`, "1", `{"a":1}[]`} {
		err := s.Write("place", doc)
		if !errors.Is(err, errInvalidJSON) {
			t.Fatalf("doc %q err %v", doc, err)
		}
	}
	if err := s.Write(" ", `{}`); !errors.Is(err, errEmptyName) {
		t.Fatalf("empty name %v", err)
	}
	got, ok, err := s.Read("place")
	if err != nil || !ok || string(got) != `{"city":"Киров"}` {
		t.Fatalf("document changed to %s ok=%v err=%v", got, ok, err)
	}
}

func TestMissingFile(t *testing.T) {
	s := openStore(t, t.TempDir())
	got, ok, err := s.Read("place")
	if err != nil || ok || got != nil {
		t.Fatalf("got %s ok=%v err=%v", got, ok, err)
	}
}

func TestDisabledStore(t *testing.T) {
	for _, path := range []string{"", " ", "\t"} {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if s.dir != "" {
			t.Fatal("disabled store opened a directory")
		}
		got, ok, err := s.Read("place")
		if err != nil || ok || got != nil {
			t.Fatalf("path %q got %s ok=%v err=%v", path, got, ok, err)
		}
		if err := s.Write("place", `{"city":"Киров"}`); !errors.Is(err, errDisabled) {
			t.Fatalf("path %q write %v", path, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLegacyPlaceCopiedOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "variacore.db")
	writePlaceDB(t, path, "Киров", 58.59809, 49.65783)

	s := openStore(t, dir)
	if err := s.ImportLegacyPlace(""); err != nil {
		t.Fatal(err)
	}
	assertPlace(t, s, "Киров", 58.59809, 49.65783)
	if err := s.Write("place", `{"country":"Россия","city":"Казань","latitude":55.79,"longitude":49.12}`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db := openSQL(t, path)
	if _, err := db.Exec(`UPDATE place SET city = 'Москва', latitude = 55.75, longitude = 37.62 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s = openStore(t, dir)
	if err := s.ImportLegacyPlace(path); err != nil {
		t.Fatal(err)
	}
	assertPlace(t, s, "Казань", 55.79, 49.12)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db = openSQL(t, path)
	defer db.Close()
	var city string
	var lat, lon float64
	if err := db.QueryRow(`SELECT city, latitude, longitude FROM place WHERE id = 1`).Scan(&city, &lat, &lon); err != nil {
		t.Fatal(err)
	}
	if city != "Москва" || math.Abs(lat-55.75) > 0.001 || math.Abs(lon-37.62) > 0.001 {
		t.Fatalf("place table was written: %s %g %g", city, lat, lon)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "place.json")); err != nil {
		t.Fatal(err)
	}
}

func TestExistingPlaceSkipsLegacyDatabase(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	if err := s.Write("place", `{"country":"Россия","city":"Казань","latitude":55.79,"longitude":49.12}`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	writePlaceDB(t, filepath.Join(dir, "variacore.db"), "Киров", 58.6, 49.66)
	s = openStore(t, dir)
	if err := s.ImportLegacyPlace(""); err != nil {
		t.Fatal(err)
	}
	assertPlace(t, s, "Казань", 55.79, 49.12)
}

func TestSiblingDatabaseImportsPlace(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data", "variacore.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db := openSQL(t, dbPath)
	if _, err := db.Exec(`CREATE TABLE skill_state (
		name TEXT PRIMARY KEY,
		doc TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	doc := `{"country":"Россия","city":"Киров","latitude":58.6,"longitude":49.66}`
	if _, err := db.Exec(`INSERT INTO skill_state (name, doc, updated_at) VALUES ('weather', ?, '2026-11-06T16:00:00Z')`, doc); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := openStore(t, dir)
	if err := s.ImportLegacyPlace(""); err != nil {
		t.Fatal(err)
	}
	assertPlace(t, s, "Киров", 58.6, 49.66)
}

func TestWeatherSkillDirImportsDataDatabase(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "data", "variacore.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writePlaceDB(t, dbPath, "Киров", 58.59809, 49.65783)
	t.Chdir(root)
	s := openStore(t, filepath.Join("skills", "weather"))
	if err := s.ImportLegacyPlace(""); err != nil {
		t.Fatal(err)
	}
	assertPlace(t, s, "Киров", 58.59809, 49.65783)
	if _, err := os.Stat(filepath.Join(root, "skills", "weather", "data", "place.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSeparateFilesKeepEachConcern(t *testing.T) {
	dir := t.TempDir()
	weather := openStore(t, dir)
	notes := openStore(t, dir)
	if err := weather.Write("place", `{"country":"Россия","city":"Киров","latitude":58.6,"longitude":49.66}`); err != nil {
		t.Fatal(err)
	}
	if err := notes.Write("note", `{"text":"hi"}`); err != nil {
		t.Fatal(err)
	}
	if err := weather.Write("place", `{"country":"Россия","city":"Казань","latitude":55.79,"longitude":49.12}`); err != nil {
		t.Fatal(err)
	}
	placeRaw, err := os.ReadFile(filepath.Join(dir, "data", "place.json"))
	if err != nil {
		t.Fatal(err)
	}
	var place map[string]any
	if err := json.Unmarshal(placeRaw, &place); err != nil {
		t.Fatal(err)
	}
	if place["city"] != "Казань" {
		t.Fatalf("place %s", placeRaw)
	}
	noteRaw, err := os.ReadFile(filepath.Join(dir, "data", "note.json"))
	if err != nil {
		t.Fatal(err)
	}
	var note map[string]any
	if err := json.Unmarshal(noteRaw, &note); err != nil {
		t.Fatal(err)
	}
	if note["text"] != "hi" {
		t.Fatalf("note %s", noteRaw)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".db") {
			t.Fatalf("extra database %s", entry.Name())
		}
	}
}

func TestMigrateSharedCopiesAndLeavesTheOldFile(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "data", "skills.json")
	if err := os.MkdirAll(filepath.Dir(shared), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{
  "weather": {"country":"Россия","city":"Киров","latitude":58.59809,"longitude":49.65783},
  "currency": {"code":"USD"},
  "notes": {"text":"hi"}
}
`)
	if err := os.WriteFile(shared, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateShared(root); err != nil {
		t.Fatal(err)
	}
	assertJSONFile(t, filepath.Join(root, "skills", "weather", "data", "place.json"), "city", "Киров")
	assertJSONFile(t, filepath.Join(root, "skills", "currency", "data", "default.json"), "code", "USD")
	assertJSONFile(t, filepath.Join(root, "skills", "notes", "data", "text.json"), "text", "hi")
	left, err := os.ReadFile(shared)
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(body) {
		t.Fatalf("shared file changed: %s", left)
	}

	place := filepath.Join(root, "skills", "weather", "data", "place.json")
	if err := os.WriteFile(place, []byte("{\"city\":\"Казань\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateShared(root); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(place)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(again), "Киров") {
		t.Fatalf("migration overwrote place.json: %s", again)
	}
}

func TestMigrateSharedUsesARegisteredCopy(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "data", "skills.json")
	if err := os.MkdirAll(filepath.Dir(shared), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"custom":{"value":"one"}}` + "\n")
	if err := os.WriteFile(shared, body, 0o644); err != nil {
		t.Fatal(err)
	}
	RegisterMigrator("custom", func(root string, doc json.RawMessage) error {
		return writeNew(filepath.Join(root, "skills", "custom"), "kept", doc)
	})
	t.Cleanup(func() { RegisterMigrator("custom", nil) })
	if err := MigrateShared(root); err != nil {
		t.Fatal(err)
	}
	assertJSONFile(t, filepath.Join(root, "skills", "custom", "data", "kept.json"), "value", "one")
	if _, err := os.Stat(filepath.Join(root, "skills", "custom", "data", "value.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default split also wrote value.json: %v", err)
	}
}

func openStore(t *testing.T, skillDir string) *Store {
	t.Helper()
	s, err := Open(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func openSQL(t *testing.T, path string) *sql.DB {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file://"+abs+"?mode=rwc&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func writePlaceDB(t *testing.T, path, city string, lat, lon float64) {
	t.Helper()
	db := openSQL(t, path)
	if _, err := db.Exec(`CREATE TABLE place (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		country TEXT NOT NULL,
		city TEXT NOT NULL,
		latitude REAL NOT NULL,
		longitude REAL NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO place (id, country, city, latitude, longitude, updated_at)
		VALUES (1, 'Россия', ?, ?, ?, '2026-11-06T16:31:28Z')`, city, lat, lon); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertPlace(t *testing.T, s *Store, city string, lat, lon float64) {
	t.Helper()
	raw, ok, err := s.Read("place")
	if err != nil || !ok {
		t.Fatalf("place ok=%v err=%v", ok, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) != 4 || doc["country"] != "Россия" || doc["city"] != city {
		t.Fatalf("document %s", raw)
	}
	gotLat, _ := doc["latitude"].(float64)
	gotLon, _ := doc["longitude"].(float64)
	if math.Abs(gotLat-lat) > 0.000001 || math.Abs(gotLon-lon) > 0.000001 {
		t.Fatalf("coords %s", raw)
	}
}

func assertJSONFile(t *testing.T, path, key string, want any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if want == nil {
		if _, ok := doc[key]; !ok {
			t.Fatalf("%s missing %s: %s", path, key, raw)
		}
		return
	}
	if doc[key] != want {
		t.Fatalf("%s %s = %#v", path, key, doc[key])
	}
}
