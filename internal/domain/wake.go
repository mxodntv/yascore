package domain

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Wake removes the configured name from the start of an utterance.
type Wake interface {
	Strip(text string) string
}

// Heard reports whether text contains a configured wake word as its own token.
// The word may sit anywhere, so a renamed assistant does not need a baked spotter model.
func Heard(text string, words []string) bool {
	set := map[string]struct{}{}
	for _, word := range words {
		p := strings.ToLower(strings.TrimSpace(word))
		if p != "" {
			set[p] = struct{}{}
		}
	}
	if len(set) == 0 {
		return false
	}
	for _, tok := range tokens(text) {
		if _, ok := set[tok]; ok {
			return true
		}
	}
	return false
}

func tokens(text string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// Strip drops leading wake words. A line of two wake words and a command keeps the command.
// A bare name with nothing after it is kept, matching the previous Python helper.
func Strip(text string, words []string) string {
	raw := strings.TrimSpace(text)
	for raw != "" {
		next := stripOnce(raw, words)
		if next == raw {
			return raw
		}
		raw = next
	}
	return ""
}

func stripOnce(raw string, words []string) string {
	lowered := strings.ToLower(raw)
	sorted := append([]string(nil), words...)
	sort.Slice(sorted, func(i, j int) bool {
		return len(strings.TrimSpace(sorted[i])) > len(strings.TrimSpace(sorted[j]))
	})
	for _, word := range sorted {
		p := strings.ToLower(strings.TrimSpace(word))
		if p == "" {
			continue
		}
		if strings.HasPrefix(lowered, p) && wakeBoundary(raw, len(p)) {
			rest := strings.TrimLeft(raw[len(p):], " ,.:;!-")
			if rest == "" {
				return raw
			}
			return rest
		}
		re := regexp.MustCompile(`^(?:эй|слушай),?\s+` + regexp.QuoteMeta(p))
		if loc := re.FindStringIndex(lowered); loc != nil && wakeBoundary(raw, loc[1]) {
			rest := strings.TrimLeft(raw[loc[1]:], " ,.:;!-")
			if rest == "" {
				return raw
			}
			return rest
		}
	}
	return raw
}

// wakeBoundary reports that s[n:] does not continue the wake word.
// A configured word strips. A longer word that only starts with it does not.
func wakeBoundary(s string, n int) bool {
	if n < 0 || n > len(s) {
		return false
	}
	if n == len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[n:])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}
