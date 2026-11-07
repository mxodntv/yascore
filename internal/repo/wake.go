package repo

import "yascore/internal/domain"

// TextWake strips the configured name.
type TextWake struct {
	words []string
}

// NewTextWake returns a wake stripper. words come from config.
func NewTextWake(words []string) *TextWake {
	return &TextWake{words: append([]string(nil), words...)}
}

// Strip implements domain.Wake.
func (w *TextWake) Strip(text string) string {
	return domain.Strip(text, w.words)
}

// Words are the configured wake names. The core uses them to detect a wake
// token anywhere in an accepted utterance.
func (w *TextWake) Words() []string {
	if w == nil {
		return nil
	}
	return append([]string(nil), w.words...)
}
