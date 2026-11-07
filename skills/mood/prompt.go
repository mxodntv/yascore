package mood

import (
	"context"
	"strings"
)

// BuildPrompt appends the active mood text to base.
// The base prompt stays in front. A missing file leaves base unchanged and creates nothing.
func BuildPrompt(base, skillDir string) string {
	id, ok, err := savedID(skillDir)
	if err != nil || !ok {
		return base
	}
	text, ok := Prompt(id)
	if !ok || strings.TrimSpace(text) == "" {
		return base
	}
	base = strings.TrimRight(base, "\n")
	if strings.TrimSpace(base) == "" {
		return text
	}
	return base + "\n\n" + text
}

func savedID(skillDir string) (string, bool, error) {
	store, err := OpenStore(skillDir)
	if err != nil {
		return "", false, err
	}
	defer store.Close()
	return store.Load(context.Background())
}
