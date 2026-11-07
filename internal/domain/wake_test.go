package domain

import (
	"strings"
	"testing"
)

func TestStripWake(t *testing.T) {
	name, words := exampleAssistant(t)
	lower := strings.ToLower(name)
	upper := strings.ToUpper(name)
	stuck := name + "г"
	cases := []struct {
		in, out string
	}{
		{"", ""},
		{"   ", ""},
		{name + ", включи свет", "включи свет"},
		{upper + " привет", "привет"},
		{"эй " + lower + ", который час", "который час"},
		{"слушай, " + lower + " как дела", "как дела"},
		{name, name},
		{"просто фраза", "просто фраза"},
		{name + ", " + name + ", стоп!", "стоп!"},
		{stuck + " приплыл", stuck + " приплыл"},
		{name + ", стоп", "стоп"},
		{"эй " + strings.ToLower(stuck) + " на рейде", "эй " + strings.ToLower(stuck) + " на рейде"},
	}
	for _, tc := range cases {
		got := Strip(tc.in, words)
		if got != tc.out {
			t.Errorf("Strip(%q)=%q want %q", tc.in, got, tc.out)
		}
	}
}

func TestHeardWakeWord(t *testing.T) {
	name, words := exampleAssistant(t)
	if !Heard(name+", который час", words) {
		t.Fatal("leading")
	}
	if !Heard("скажи "+strings.ToLower(name)+" время", words) {
		t.Fatal("middle")
	}
	if Heard(strings.ToLower(name)+"г на столе", words) {
		t.Fatal("prefix")
	}
	if Heard("просто фраза", words) {
		t.Fatal("none")
	}
}

func TestStripPrefersLongerWord(t *testing.T) {
	name, _ := exampleAssistant(t)
	runes := []rune(name)
	if len(runes) < 2 {
		t.Fatal("name is too short to test a longer match")
	}
	shorter := string(runes[:len(runes)-1])
	got := Strip(name+" привет", []string{shorter, name})
	if got != "привет" {
		t.Fatalf("got %q", got)
	}
}
