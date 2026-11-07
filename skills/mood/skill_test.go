package mood

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSwitchAngryPersistsAndPromptIncludesAddendum(t *testing.T) {
	name, _ := exampleAssistant(t)
	dir := t.TempDir()
	base := "Ты " + name + ". Трек не называй."
	skill := open(t, dir)

	if got := BuildPrompt(base, dir); got != base {
		t.Fatalf("before switch %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "mood.json")); !os.IsNotExist(err) {
		t.Fatal("file created before a switch")
	}

	got, err := skill.Ask(context.Background(), "злая")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Хорошо, буду злой." {
		t.Fatalf("confirm %q", got)
	}
	if id := mustSaved(t, skill); id != "злая" {
		t.Fatalf("saved %q", id)
	}
	built := BuildPrompt(base, dir)
	angry, ok := Prompt("злая")
	if !ok {
		t.Fatal("missing angry prompt")
	}
	if !strings.HasPrefix(built, base) || !strings.Contains(built, angry) {
		t.Fatalf("prompt %q", built)
	}
	if strings.Contains(built, "\n-") || strings.Contains(angry, "#") {
		t.Fatalf("markdown in %q", built)
	}
}

func TestSwitchSweetReplacesAngry(t *testing.T) {
	dir := t.TempDir()
	skill := open(t, dir)
	ctx := context.Background()
	if _, err := skill.Ask(ctx, "злая"); err != nil {
		t.Fatal(err)
	}
	got, err := skill.Ask(ctx, "милая")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Хорошо, буду милой." {
		t.Fatalf("confirm %q", got)
	}
	if id := mustSaved(t, skill); id != "милая" {
		t.Fatalf("saved %q", id)
	}
	raw := mustFile(t, dir)
	if strings.Contains(raw, "злая") {
		t.Fatalf("old mood left in file %s", raw)
	}
	var doc struct {
		Mood string `json:"mood"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Mood != "милая" {
		t.Fatalf("doc %+v", doc)
	}
	base := "База."
	built := BuildPrompt(base, dir)
	sweet, _ := Prompt("милая")
	angry, _ := Prompt("злая")
	if !strings.Contains(built, sweet) || strings.Contains(built, angry) {
		t.Fatalf("prompt %q", built)
	}
}

func TestUnknownMoodDoesNotChangeSaved(t *testing.T) {
	dir := t.TempDir()
	skill := open(t, dir)
	ctx := context.Background()
	if _, err := skill.Ask(ctx, "злая"); err != nil {
		t.Fatal(err)
	}
	got, err := skill.Ask(ctx, "космическая")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"доброжелательной", "злой", "милой", "весёлой", "спокойной"} {
		if !strings.Contains(got, part) {
			t.Fatalf("known list %q missing %s", got, part)
		}
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "- ") {
		t.Fatalf("list %q", got)
	}
	if id := mustSaved(t, skill); id != "злая" {
		t.Fatalf("saved changed to %q", id)
	}
	angry, _ := Prompt("злая")
	if !strings.Contains(BuildPrompt("База.", dir), angry) {
		t.Fatal("prompt dropped the saved mood")
	}
}

func TestUnknownMoodDoesNotCreateFile(t *testing.T) {
	dir := t.TempDir()
	skill := open(t, dir)
	if _, err := skill.Ask(context.Background(), "непонятная"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "mood.json")); !os.IsNotExist(err) {
		t.Fatal("unknown mood created a file")
	}
}

func TestNoSavedMoodLeavesBasePrompt(t *testing.T) {
	name, _ := exampleAssistant(t)
	dir := filepath.Join(t.TempDir(), "missing")
	base := "Ты " + name + ". Трек не называй."
	if got := BuildPrompt(base, dir); got != base {
		t.Fatalf("prompt %q", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("prompt builder created a directory")
	}
	askDir := t.TempDir()
	skill := open(t, askDir)
	got, err := skill.Ask(context.Background(), "какое настроение")
	if err != nil {
		t.Fatal(err)
	}
	if got != nonePhrase {
		t.Fatalf("current %q", got)
	}
	if _, err := os.Stat(filepath.Join(askDir, "data", "mood.json")); !os.IsNotExist(err) {
		t.Fatal("query created a file")
	}
}

func TestAliasesMapToMood(t *testing.T) {
	cases := []struct {
		in      string
		id      string
		confirm string
	}{
		{"доброй", "доброжелательная", "Хорошо, буду доброжелательной."},
		{"доброй будь", "доброжелательная", "Хорошо, буду доброжелательной."},
		{"злой", "злая", "Хорошо, буду злой."},
		{"милой", "милая", "Хорошо, буду милой."},
		{"милой будь", "милая", "Хорошо, буду милой."},
		{"весёлой", "весёлая", "Хорошо, буду весёлой."},
		{"спокойной", "спокойная", "Хорошо, буду спокойной."},
		{"стань злой", "злая", "Хорошо, буду злой."},
		{"стань доброжелательной", "доброжелательная", "Хорошо, буду доброжелательной."},
		{"стань милой", "милая", "Хорошо, буду милой."},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			dir := t.TempDir()
			skill := open(t, dir)
			got, err := skill.Ask(context.Background(), tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.confirm {
				t.Fatalf("confirm %q", got)
			}
			if id := mustSaved(t, skill); id != tc.id {
				t.Fatalf("saved %q", id)
			}
			text, ok := Prompt(tc.id)
			if !ok || !strings.Contains(BuildPrompt("База.", dir), text) {
				t.Fatal("alias did not reach the prompt")
			}
		})
	}
}

func TestQuerySpeaksCurrent(t *testing.T) {
	skill := open(t, t.TempDir())
	ctx := context.Background()
	if _, err := skill.Ask(ctx, "стань злой"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"какое настроение", "какая ты сейчас", ""} {
		got, err := skill.Handle(ctx, "set_mood", `{"mood":`+jsonString(q)+`}`)
		if err != nil {
			t.Fatal(err)
		}
		if got != "Сейчас я злая." {
			t.Fatalf("%q -> %q", q, got)
		}
	}
	if id := mustSaved(t, skill); id != "злая" {
		t.Fatalf("query wrote %q", id)
	}
}

func TestHandleSwitch(t *testing.T) {
	dir := t.TempDir()
	skill := open(t, dir)
	got, err := skill.Handle(context.Background(), "set_mood", `{"mood":"злой"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Хорошо, буду злой." {
		t.Fatalf("confirm %q", got)
	}
	if id := mustSaved(t, skill); id != "злая" {
		t.Fatalf("saved %q", id)
	}
}

func open(t *testing.T, dir string) *Skill {
	t.Helper()
	skill, err := New(Settings{Database: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = skill.Close() })
	return skill
}

func mustSaved(t *testing.T, skill *Skill) string {
	t.Helper()
	id, ok, err := skill.store.Load(context.Background())
	if err != nil || !ok {
		t.Fatalf("load %q ok=%v err=%v", id, ok, err)
	}
	return id
}

func mustFile(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "data", "mood.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
