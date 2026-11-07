// Package skillstate stores one JSON file per concern inside a skill directory.
// Files live in <skillDir>/data/<name>.json. Skills import this package directly.
// It does not import the assistant core.
package skillstate

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	_ "modernc.org/sqlite"
)

const legacyWeatherName = "weather"

var (
	errEmptyName   = errors.New("skillstate: empty name")
	errInvalidJSON = errors.New("skillstate: document must be a json object")
	errDisabled    = errors.New("skillstate: store disabled")
)

// Store is one skill's data directory.
// An empty directory leaves the store disabled: Read reports not found and Write returns an error.
type Store struct {
	dir string // <skillDir>/data
}

// Open prepares skillDir for documents under skillDir/data.
// An empty skillDir returns a disabled store and does not create a directory.
// The data directory is created on the first Write.
func Open(skillDir string) (*Store, error) {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return &Store{}, nil
	}
	return &Store{dir: filepath.Join(skillDir, "data")}, nil
}

// Close releases nothing. Each Write is its own file.
func (s *Store) Close() error { return nil }

// Read returns the JSON object stored as data/<name>.json.
// The boolean is false when the file is missing.
func (s *Store) Read(name string) (json.RawMessage, bool, error) {
	name, err := cleanName(name)
	if err != nil {
		return nil, false, err
	}
	if s == nil || s.dir == "" {
		return nil, false, nil
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("skillstate: %w", err)
	}
	body := bytes.TrimSpace(raw)
	if len(body) == 0 {
		return nil, false, nil
	}
	compact, err := compactObject(string(body))
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(compact), true, nil
}

// Write replaces data/<name>.json. object must be a JSON object:
// a struct, a map, or the JSON text of an object.
func (s *Store) Write(name string, object any) error {
	return s.write(name, object, false)
}

// writeIfAbsent stores object only when the file is not already there.
// Migration uses it so a newer file is never replaced by the old shared document.
func (s *Store) writeIfAbsent(name string, object any) error {
	return s.write(name, object, true)
}

func (s *Store) write(name string, object any, onlyIfAbsent bool) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	if s == nil || s.dir == "" {
		return errDisabled
	}
	compact, err := normalize(object)
	if err != nil {
		return err
	}
	body, err := pretty(compact)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("skillstate: %w", err)
	}
	path := filepath.Join(s.dir, name+".json")
	unlock, err := lockFile(path)
	if err != nil {
		return fmt.Errorf("skillstate: %w", err)
	}
	defer unlock()
	if onlyIfAbsent {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("skillstate: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("skillstate: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("skillstate: %w", err)
	}
	return nil
}

// Remove deletes data/<name>.json. A missing file is not an error.
func (s *Store) Remove(name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	if s == nil || s.dir == "" {
		return errDisabled
	}
	path := filepath.Join(s.dir, name+".json")
	if _, err := os.Stat(s.dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	unlock, err := lockFile(path)
	if err != nil {
		return fmt.Errorf("skillstate: %w", err)
	}
	defer unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("skillstate: %w", err)
	}
	return nil
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", errEmptyName
	}
	return name, nil
}

func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func normalize(object any) (string, error) {
	switch v := object.(type) {
	case nil:
		return "", errInvalidJSON
	case string:
		return compactObject(v)
	case []byte:
		return compactObject(string(v))
	case json.RawMessage:
		return compactObject(string(v))
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return "", errInvalidJSON
		}
		return compactObject(string(raw))
	}
}

func pretty(compact string) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(compact), "", "  "); err != nil {
		return nil, errInvalidJSON
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func compactObject(doc string) (string, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(strings.TrimSpace(doc))); err != nil {
		return "", errInvalidJSON
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &obj); err != nil || obj == nil {
		return "", errInvalidJSON
	}
	return buf.String(), nil
}

// Migrator copies one document from data/skills.json into that skill's files.
// A skill registers its own. This package does not name skills.
type Migrator func(root string, doc json.RawMessage) error

var (
	migratorsMu sync.Mutex
	migrators   = map[string]Migrator{}
)

// RegisterMigrator installs a custom copy for one key in data/skills.json.
// A skill calls this from init. A nil function removes the key.
// Weather and currency stay on their own copy and are not replaced by this.
func RegisterMigrator(key string, fn Migrator) {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, `/\`) || key == "." || key == ".." {
		return
	}
	migratorsMu.Lock()
	defer migratorsMu.Unlock()
	if fn == nil {
		delete(migrators, key)
		return
	}
	migrators[key] = fn
}

func migratorFor(key string) Migrator {
	migratorsMu.Lock()
	defer migratorsMu.Unlock()
	return migrators[key]
}

// MigrateShared copies documents out of root/data/skills.json into
// skills/<skill>/data/<concern>.json. Existing files are left as they are.
// The old file is not deleted.
// An empty root means the process working directory, which is the repo root
// when yascore is started from the service.
func MigrateShared(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	path := filepath.Join(root, "data", "skills.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("skillstate: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var docs map[string]json.RawMessage
	if err := json.Unmarshal(raw, &docs); err != nil || docs == nil {
		return fmt.Errorf("skillstate: %w", errInvalidJSON)
	}
	for key, doc := range docs {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, `/\`) || key == "." || key == ".." {
			continue
		}
		if err := migrateDoc(root, key, doc); err != nil {
			return err
		}
	}
	return nil
}

func migrateDoc(root, key string, doc json.RawMessage) error {
	switch key {
	case "weather":
		return writeNew(filepath.Join(root, "skills", "weather"), "place", doc)
	case "currency":
		return writeNew(filepath.Join(root, "skills", "currency"), "default", doc)
	}
	if fn := migratorFor(key); fn != nil {
		return fn(root, doc)
	}
	return splitConcerns(filepath.Join(root, "skills", key), doc)
}

func splitConcerns(skillDir string, doc json.RawMessage) error {
	obj, err := objectKeys(doc)
	if err != nil {
		return err
	}
	for key, raw := range obj {
		if err := writeNew(skillDir, key, wrapKey(key, raw)); err != nil {
			return err
		}
	}
	return nil
}

func objectKeys(doc json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(doc, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("skillstate: %w", errInvalidJSON)
	}
	return obj, nil
}

func wrapKey(key string, raw json.RawMessage) json.RawMessage {
	body, err := json.Marshal(map[string]json.RawMessage{key: raw})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return body
}

func writeNew(skillDir, name string, doc json.RawMessage) error {
	s, err := Open(skillDir)
	if err != nil {
		return err
	}
	return s.writeIfAbsent(name, doc)
}

// ImportLegacyPlace copies the weather place out of variacore.db into place.json
// when place.json is missing. The database is only read.
// dbPath is an explicit database file. An empty dbPath looks for variacore.db
// beside the skill directory and, for skills/weather, in data/variacore.db.
func (s *Store) ImportLegacyPlace(dbPath string) error {
	if s == nil || s.dir == "" {
		return nil
	}
	if _, ok, err := s.Read("place"); err != nil || ok {
		return err
	}
	skillDir := filepath.Dir(s.dir)
	candidates := make([]string, 0, 4)
	if strings.TrimSpace(dbPath) != "" {
		candidates = append(candidates, dbPath)
	}
	candidates = append(candidates,
		filepath.Join(skillDir, "variacore.db"),
		filepath.Join(s.dir, "variacore.db"),
	)
	if isWeatherSkill(skillDir) {
		candidates = append(candidates, filepath.Join("data", "variacore.db"))
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		body, ok, err := readLegacyWeather(candidate)
		if err != nil || !ok {
			return err
		}
		return s.Write("place", json.RawMessage(body))
	}
	return nil
}

func isWeatherSkill(skillDir string) bool {
	clean := filepath.Clean(skillDir)
	suffix := filepath.Join("skills", "weather")
	return clean == suffix || strings.HasSuffix(clean, string(os.PathSeparator)+suffix)
}

// readLegacyWeather reads the old weather row. The file name variacore.db is the
// one the caller passes; this function does not rename it.
func readLegacyWeather(path string) (string, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("skillstate: legacy: %w", err)
	}
	db, err := sql.Open("sqlite", "file://"+abs+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return "", false, fmt.Errorf("skillstate: legacy: %w", err)
	}
	defer db.Close()
	var doc string
	err = db.QueryRow(`SELECT doc FROM skill_state WHERE name = ?`, legacyWeatherName).Scan(&doc)
	if err == nil && strings.TrimSpace(doc) != "" {
		compact, compactErr := compactObject(doc)
		if compactErr != nil {
			return "", false, compactErr
		}
		return compact, true, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !isNoSuchTable(err) {
		return "", false, fmt.Errorf("skillstate: legacy: %w", err)
	}
	var country, city string
	var lat, lon float64
	err = db.QueryRow(`SELECT country, city, latitude, longitude FROM place WHERE id = 1`).Scan(&country, &city, &lat, &lon)
	if errors.Is(err, sql.ErrNoRows) || isNoSuchTable(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("skillstate: legacy: %w", err)
	}
	body, err := json.Marshal(struct {
		Country   string  `json:"country"`
		City      string  `json:"city"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}{country, city, lat, lon})
	if err != nil {
		return "", false, fmt.Errorf("skillstate: legacy: %w", err)
	}
	return string(body), true, nil
}

func isNoSuchTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}
