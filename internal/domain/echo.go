package domain

import (
	"strings"
	"unicode"
)

// EchoStrip drops tokens from heard that also appear in spoken.
// Matching ignores case and punctuation. The order of what remains is kept.
// An empty spoken line returns heard trimmed. Short player and stop commands
// are never removed, so a real command mixed into the echo still survives.
func EchoStrip(heard, spoken string) string {
	if strings.TrimSpace(spoken) == "" {
		return strings.TrimSpace(heard)
	}
	spokenSet := map[string]struct{}{}
	for _, tok := range strings.Fields(spoken) {
		if norm := normToken(tok); norm != "" {
			spokenSet[norm] = struct{}{}
		}
	}
	if len(spokenSet) == 0 {
		return strings.TrimSpace(heard)
	}
	var kept []string
	for _, tok := range strings.Fields(heard) {
		norm := normToken(tok)
		if norm == "" {
			kept = append(kept, tok)
			continue
		}
		if _, drop := spokenSet[norm]; drop && !protectedCommand(norm) {
			continue
		}
		kept = append(kept, tok)
	}
	return strings.Join(kept, " ")
}

func protectedCommand(norm string) bool {
	switch norm {
	case "стоп", "пауза", "дальше", "назад", "продолжи", "хватит", "замолчи", "остановись", "тише", "помолчи":
		return true
	default:
		return false
	}
}

func normToken(tok string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(tok) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PureEcho reports that heard is the assistant's own speech played back.
// Every content token is in spoken, and the line is either at least three
// tokens or it repeats the whole spoken line. One or two words that do not
// cover that line are a follow-up («да», «эту», «его»), not an echo.
func PureEcho(heard, spoken string) bool {
	h := contentTokens(heard)
	s := contentTokens(spoken)
	if len(h) == 0 || len(s) == 0 {
		return false
	}
	spokenSet := map[string]struct{}{}
	for _, tok := range s {
		spokenSet[tok] = struct{}{}
	}
	heardSet := map[string]struct{}{}
	for _, tok := range h {
		if _, ok := spokenSet[tok]; !ok {
			return false
		}
		heardSet[tok] = struct{}{}
	}
	if len(h) >= 3 {
		return true
	}
	for _, tok := range s {
		if _, ok := heardSet[tok]; !ok {
			return false
		}
	}
	return true
}

func contentTokens(s string) []string {
	var out []string
	for _, field := range strings.Fields(s) {
		if tok := normToken(field); tok != "" {
			out = append(out, tok)
		}
	}
	return out
}
