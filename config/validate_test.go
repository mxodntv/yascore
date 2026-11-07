package config

import (
	"os"
	"strings"
	"testing"
)

func TestValidateRejectsUnknownEngine(t *testing.T) {
	cfg := mustExample(t)
	cfg.Core.LLM.Engine = "openai"
	if _, err := Validate(cfg); err == nil {
		t.Fatal("expected unknown engine error")
	}
}

func TestValidateRejectsEmptyName(t *testing.T) {
	cfg := mustExample(t)
	cfg.Core.Name = "  "
	if _, err := Validate(cfg); err == nil {
		t.Fatal("expected empty name error")
	}
}

func TestValidateRejectsSileroWithoutURL(t *testing.T) {
	cfg := mustExample(t)
	cfg.Core.TTS.URL = ""
	if _, err := Validate(cfg); err == nil {
		t.Fatal("expected tts url error")
	}
}

func TestValidateRejectsWakeWithoutWords(t *testing.T) {
	cfg := mustExample(t)
	cfg.Core.Wake.Words = nil
	if _, err := Validate(cfg); err == nil {
		t.Fatal("expected wake words error")
	}
}

func TestValidateRejectsVoiceVolume(t *testing.T) {
	cfg := mustExample(t)
	cfg.Core.TTS.Volume = 140
	if _, err := Validate(cfg); err == nil {
		t.Fatal("expected volume error")
	}
}

func TestValidateRejectsWeatherURL(t *testing.T) {
	cfg := mustExample(t)
	cfg.Skills.Weather.APIBase = "not a url"
	if _, err := Validate(cfg); err == nil {
		t.Fatal("expected weather url error")
	}
}

func TestValidateRejectsBadListen(t *testing.T) {
	cfg := mustExample(t)
	cfg.Core.Server.Listen = "8091"
	if _, err := Validate(cfg); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestValidateWarnsWhenSkillHostEmpty(t *testing.T) {
	cfg := mustExample(t)
	cfg.Skills.Weather.APIBase = ""
	on, err := Validate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if on.Weather {
		t.Fatal("weather should be off")
	}
}

func mustExample(t *testing.T) Config {
	t.Helper()
	cfg, err := LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func osWrite(path string, body []byte) error {
	return os.WriteFile(path, body, 0o644)
}

func TestNameAppearsOnceInPrompt(t *testing.T) {
	cfg := mustExample(t)
	if strings.Count(cfg.Core.Prompt, cfg.Core.Name) != 1 {
		t.Fatalf("name count %d", strings.Count(cfg.Core.Prompt, cfg.Core.Name))
	}
}
