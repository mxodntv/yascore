package musicyoutube

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yascore/internal/skillstate"
)

func TestSharedDocumentCopiesLikesHistoryAndQueue(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "data", "skills.json")
	if err := os.MkdirAll(filepath.Dir(shared), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{
  "musicyoutube": {
    "likes": [{"title":"Get Lucky","artists":"Daft Punk"}],
    "recent": {"title":"Instant Crush"},
    "tracks": [{"title":"Alpha"},{"title":"Beta"}],
    "index": 1
  }
}
`)
	if err := os.WriteFile(shared, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := skillstate.MigrateShared(root); err != nil {
		t.Fatal(err)
	}
	likes := readObject(t, filepath.Join(root, "skills", "musicyoutube", "data", "likes.json"))
	if _, ok := likes["likes"]; !ok {
		t.Fatalf("likes %v", likes)
	}
	history := readObject(t, filepath.Join(root, "skills", "musicyoutube", "data", "history.json"))
	recent, _ := history["recent"].(map[string]any)
	if recent["title"] != "Instant Crush" {
		t.Fatalf("history %v", history)
	}
	queue := readObject(t, filepath.Join(root, "skills", "musicyoutube", "data", "queue.json"))
	tracks, _ := queue["tracks"].([]any)
	if len(tracks) != 2 || queue["index"] != float64(1) {
		t.Fatalf("queue %v", queue)
	}
	left, err := os.ReadFile(shared)
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(body) {
		t.Fatalf("shared file changed: %s", left)
	}

	likesPath := filepath.Join(root, "skills", "musicyoutube", "data", "likes.json")
	if err := os.WriteFile(likesPath, []byte("{\"likes\":[]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := skillstate.MigrateShared(root); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(likesPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(again), "Get Lucky") {
		t.Fatalf("migration overwrote likes.json: %s", again)
	}
}

func readObject(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}
