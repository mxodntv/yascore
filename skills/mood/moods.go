package mood

import (
	"strings"

	"yascore/config"
)

// mood is one prewritten tone. The text is appended to the base system prompt.
// It does not replace that prompt.
type mood struct {
	id     string
	will   string // «буду злой»
	prompt string
	keys   []string
}

const (
	nonePhrase = "Сейчас я как обычно."
	failPhrase = "Не удалось запомнить настроение."
)

// catalog order is the order spoken when the name is unknown.
// Install fills it from the loaded config.
var (
	catalog []mood
	byKey   = map[string]mood{}
)

// Install replaces the tone catalog with the tones from config.
func Install(tones []config.MoodTone) {
	catalog = make([]mood, 0, len(tones))
	for _, tone := range tones {
		keys := append([]string(nil), tone.Keys...)
		if len(keys) == 0 && strings.TrimSpace(tone.ID) != "" {
			keys = []string{tone.ID}
		}
		catalog = append(catalog, mood{
			id:     strings.TrimSpace(tone.ID),
			will:   strings.TrimSpace(tone.Will),
			prompt: strings.TrimSpace(tone.Prompt),
			keys:   keys,
		})
	}
	byKey = mapMoods(catalog)
}

func mapMoods(list []mood) map[string]mood {
	out := make(map[string]mood, len(list)*2)
	for _, m := range list {
		for _, key := range m.keys {
			out[fold(key)] = m
		}
	}
	return out
}

// Prompt is the addendum for a saved mood id. The boolean is false when id is unknown.
func Prompt(id string) (string, bool) {
	m, ok := byKey[fold(id)]
	if !ok || fold(m.id) != fold(id) {
		return "", false
	}
	return m.prompt, true
}

func (m mood) confirm() string { return "Хорошо, буду " + m.will + "." }

func (m mood) current() string { return "Сейчас я " + m.id + "." }

func knownPhrase() string {
	parts := make([]string, len(catalog))
	for i, m := range catalog {
		parts[i] = m.will
	}
	if len(parts) == 0 {
		return "Такого настроения нет."
	}
	if len(parts) == 1 {
		return "Такого настроения нет. Могу быть " + parts[0] + "."
	}
	last := parts[len(parts)-1]
	return "Такого настроения нет. Могу быть " + strings.Join(parts[:len(parts)-1], ", ") + " или " + last + "."
}

const (
	kindQuery = iota
	kindSet
	kindUnknown
)

func kindOf(raw string) (mood, int) {
	folded := fold(raw)
	if isQuery(folded) {
		return mood{}, kindQuery
	}
	peeled := peel(raw)
	if peeled == "" || isQuery(peeled) {
		return mood{}, kindQuery
	}
	if m, ok := byKey[peeled]; ok {
		return m, kindSet
	}
	return mood{}, kindUnknown
}

func isQuery(s string) bool {
	switch s {
	case "", "какое настроение", "какое сейчас настроение", "какое у тебя настроение",
		"какая ты", "какая ты сейчас", "ты какая", "какая ты теперь":
		return true
	default:
		return strings.Contains(s, "какое настроение") || strings.Contains(s, "какая ты")
	}
}

func peel(raw string) string {
	s := fold(raw)
	for i := 0; i < 4; i++ {
		next := s
		for _, p := range []string{"стань-ка ", "стань ", "становись ", "сделайся ", "будь "} {
			if strings.HasPrefix(next, p) {
				next = strings.TrimSpace(strings.TrimPrefix(next, p))
			}
		}
		if strings.HasSuffix(next, " будь") {
			next = strings.TrimSpace(strings.TrimSuffix(next, " будь"))
		}
		if next == s {
			break
		}
		s = next
	}
	return s
}

func fold(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "ё", "е")
	s = strings.Trim(s, " \t\n\r«»\"'.,!?;:")
	return strings.Join(strings.Fields(s), " ")
}
