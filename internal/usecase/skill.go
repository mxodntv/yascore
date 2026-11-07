package usecase

import "strings"

// wantsTools is true when the utterance matches a hint advertised by a live skill.
func wantsTools(text string, hints []string) bool {
	folded := strings.ToLower(text)
	for _, hint := range hints {
		h := strings.ToLower(strings.TrimSpace(hint))
		if h != "" && strings.Contains(folded, h) {
			return true
		}
	}
	return false
}
