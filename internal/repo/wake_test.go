package repo

import (
	"strings"
	"testing"

	"yascore/config"
)

func TestTextWake(t *testing.T) {
	cfg, err := config.LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	name := cfg.Core.Name
	w := NewTextWake(cfg.Core.Wake.Words)
	if got := w.Strip("эй " + name + ", включи"); got != "включи" {
		t.Fatalf("got %q", got)
	}
	upper := strings.ToUpper(name)
	if got := w.Strip(upper); got != upper {
		t.Fatalf("bare name %q", got)
	}
	if got := w.Strip("сосед"); got != "сосед" {
		t.Fatalf("other name %q", got)
	}
}
