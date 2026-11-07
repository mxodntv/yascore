package domain

import (
	"strings"
	"testing"
	"unicode"
)

func TestEchoStrip(t *testing.T) {
	cases := []struct {
		name, heard, spoken, want string
	}{
		{
			name:  "echo only",
			heard: "Сегодня хорошая погода", spoken: "сегодня, хорошая погода!",
			want: "",
		},
		{
			name:  "echo plus stop",
			heard: "Сегодня хорошая погода стоп", spoken: "сегодня, хорошая погода!",
			want: "стоп",
		},
		{
			name:  "echo plus question",
			heard: "Сегодня хорошая погода кто поёт", spoken: "сегодня, хорошая погода!",
			want: "кто поёт",
		},
		{
			name:  "empty spoken",
			heard: "  Привет, мир!  ", spoken: "",
			want: "Привет, мир!",
		},
		{
			name:  "punctuation and case",
			heard: "ПРИВЕТ, КАК ДЕЛА?!", spoken: "привет как дела",
			want: "",
		},
		{
			name:  "order kept",
			heard: "раз два три", spoken: "два",
			want: "раз три",
		},
		{
			name:   "protected words stay",
			heard:  "привет стоп пауза дальше назад продолжи хватит замолчи остановись тише помолчи",
			spoken: "привет стоп пауза дальше назад продолжи хватит замолчи остановись тише помолчи",
			want:   "стоп пауза дальше назад продолжи хватит замолчи остановись тише помолчи",
		},
		{
			name:  "stop between echoed words",
			heard: "альфа стоп бета", spoken: "альфа бета",
			want: "стоп",
		},
		{
			name:  "empty heard",
			heard: "   ", spoken: "привет",
			want: "",
		},
		{
			name:  "whitespace spoken",
			heard: "  Привет, мир!  ", spoken: "   ",
			want: "Привет, мир!",
		},
		{
			name:  "unrelated tokens stay",
			heard: "включи свет", spoken: "сегодня пятница",
			want: "включи свет",
		},
		{
			name:  "substring is not a token",
			heard: "стопка на столе", spoken: "стоп",
			want: "стопка на столе",
		},
		{
			name:  "repeated echo dropped",
			heard: "раз раз", spoken: "раз",
			want: "",
		},
		{
			name:  "guillemets and dash",
			heard: "«Привет» — как дела?", spoken: "привет как дела",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EchoStrip(tc.heard, tc.spoken)
			if tc.spoken == "" || strings.TrimSpace(tc.spoken) == "" {
				if got != tc.want {
					t.Fatalf("EchoStrip(%q, %q)=%q want %q", tc.heard, tc.spoken, got, tc.want)
				}
				return
			}
			if !sameEchoWords(got, tc.want) {
				t.Fatalf("EchoStrip(%q, %q)=%q want %q", tc.heard, tc.spoken, got, tc.want)
			}
		})
	}
}

func TestEchoStripPunctuationOnlySpoken(t *testing.T) {
	got := EchoStrip("  Привет, мир!  ", "...")
	if got != "Привет, мир!" {
		t.Fatalf("got %q", got)
	}
	got = EchoStrip("СТОП!", "?! —")
	if !sameEchoWords(got, "стоп") {
		t.Fatalf("got %q", got)
	}
}

func TestPureEchoKeepsShortFollowUp(t *testing.T) {
	if PureEcho("да", "Да, включить эту песню?") {
		t.Fatal("да was echo")
	}
	if PureEcho("эту", "Включить эту песню?") {
		t.Fatal("эту was echo")
	}
	if PureEcho("его", "Ставлю его следующим.") {
		t.Fatal("его was echo")
	}
	if !PureEcho("Сегодня пятница.", "Сегодня пятница.") {
		t.Fatal("the whole sentence was kept")
	}
	if !PureEcho("Как у тебя?", "Дела неплохо. Как у тебя?") {
		t.Fatal("a long fragment was kept")
	}
	if PureEcho("кто поёт", "Сегодня пятница.") {
		t.Fatal("new words were echo")
	}
	if PureEcho("да", "") {
		t.Fatal("empty spoken")
	}
}

func TestEchoStripKeepsEachControlWord(t *testing.T) {
	words := []string{"стоп", "пауза", "дальше", "назад", "продолжи", "хватит", "замолчи", "остановись", "тише", "помолчи"}
	for _, word := range words {
		heard := "Сегодня " + strings.ToUpper(word) + "!"
		got := EchoStrip(heard, "сегодня")
		if !sameEchoWords(got, word) {
			t.Fatalf("EchoStrip(%q, сегодня)=%q want %q", heard, got, word)
		}
		kept := EchoStrip(word, word)
		if !sameEchoWords(kept, word) {
			t.Fatalf("EchoStrip(%q, %q)=%q", word, word, kept)
		}
	}
}

func sameEchoWords(got, want string) bool {
	gw, ww := echoWords(got), echoWords(want)
	if len(gw) != len(ww) {
		return false
	}
	for i := range gw {
		if gw[i] != ww[i] {
			return false
		}
	}
	return true
}

func echoWords(s string) []string {
	var out []string
	for _, field := range strings.Fields(strings.ToLower(s)) {
		var b strings.Builder
		for _, r := range field {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		if b.Len() > 0 {
			out = append(out, b.String())
		}
	}
	return out
}
