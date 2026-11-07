package websearch

import (
	"context"
	"testing"
)

func TestParseResults(t *testing.T) {
	html := `<a class="result__a" href="http://ex">Курс доллара</a><a class="result__snippet">90 рублей</a>`
	got := ParseResults(html)
	if got != "Курс доллара. 90 рублей" {
		t.Fatalf("got %q", got)
	}
}

func TestParseResultsEmpty(t *testing.T) {
	if ParseResults("<html></html>") != "ничего не нашлось" {
		t.Fatal(ParseResults("<html></html>"))
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	got, err := Search(context.Background(), "http://127.0.0.1", "  ")
	if err != nil || got != "пустой запрос" {
		t.Fatalf("%q %v", got, err)
	}
}
