package config

import (
	"strings"
	"testing"
)

func TestLoadExample(t *testing.T) {
	cfg, err := LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cfg.Core.Name) == "" {
		t.Fatal("empty name")
	}
	if !strings.Contains(cfg.Core.Prompt, cfg.Core.Name) {
		t.Fatal("prompt did not receive the name")
	}
	if strings.Contains(cfg.Core.Prompt, "{name}") {
		t.Fatal("name placeholder left in the prompt")
	}
	if cfg.Core.LLM.Model == "" || cfg.Core.TTS.Voice == "" || cfg.Core.STT.Engine != "faster-whisper" {
		t.Fatalf("engines %+v", cfg.Core)
	}
	on, err := Validate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !on.Weather || cfg.Skills.Weather.APIBase == "" || cfg.Skills.Weather.Database == "" {
		t.Fatalf("weather %+v on %v", cfg.Skills.Weather, on.Weather)
	}
	if !on.Currency || cfg.Skills.Currency.APIBase == "" {
		t.Fatalf("currency %+v", cfg.Skills.Currency)
	}
	if cfg.Logs.Conversations == "" || cfg.Logs.Level != "INFO" {
		t.Fatalf("logs %+v", cfg.Logs)
	}
	if len(cfg.Core.Wake.Words) != 1 || cfg.Core.Wake.Words[0] != cfg.Core.Name {
		t.Fatalf("wake %#v", cfg.Core.Wake.Words)
	}
}

func TestLoadBrokenYAML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.yml"
	if err := osWrite(path, []byte(":\n  - [")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestValidateRejectsEmptyCoreHost(t *testing.T) {
	cfg := mustExample(t)
	cfg.Core.LLM.BaseURL = ""
	if _, err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "core.llm.base_url") {
		t.Fatalf("err %v", err)
	}
}
