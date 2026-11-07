package youtubemusic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"yascore/internal/skillstate"
)

func init() {
	skillstate.RegisterMigrator("youtubemusic", migrateSharedDoc)
}

// migrateSharedDoc copies the old data/skills.json "youtubemusic" document
// into this skill's JSON files. The SQLite store imports those files once.
func migrateSharedDoc(root string, doc json.RawMessage) error {
	obj, err := jsonObject(doc)
	if err != nil {
		return err
	}
	skillDir := filepath.Join(root, "skills", "youtube-music")
	if raw, ok := obj["likes"]; ok {
		if err := writeIfMissing(skillDir, "likes", wrapJSONKey("likes", raw)); err != nil {
			return err
		}
		delete(obj, "likes")
	}
	if raw, ok := obj["recent"]; ok {
		if err := writeIfMissing(skillDir, "history", wrapJSONKey("recent", raw)); err != nil {
			return err
		}
		delete(obj, "recent")
	}
	queue := map[string]json.RawMessage{}
	for _, key := range []string{"tracks", "index", "queue"} {
		if raw, ok := obj[key]; ok {
			queue[key] = raw
			delete(obj, key)
		}
	}
	if len(queue) > 0 {
		body, err := json.Marshal(queue)
		if err != nil {
			return fmt.Errorf("youtube-music: %w", err)
		}
		if err := writeIfMissing(skillDir, "queue", json.RawMessage(body)); err != nil {
			return err
		}
	}
	for key, raw := range obj {
		if err := writeIfMissing(skillDir, key, wrapJSONKey(key, raw)); err != nil {
			return err
		}
	}
	return nil
}

func jsonObject(doc json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(doc, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("youtube-music: shared document must be a json object")
	}
	return obj, nil
}

func wrapJSONKey(key string, raw json.RawMessage) json.RawMessage {
	body, err := json.Marshal(map[string]json.RawMessage{key: raw})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return body
}

// writeIfMissing stores object only when data/<name>.json is not already there.
func writeIfMissing(skillDir, name string, object any) error {
	path := filepath.Join(skillDir, "data", name+".json")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	s, err := skillstate.Open(skillDir)
	if err != nil {
		return err
	}
	return s.Write(name, object)
}
