package usecase

import (
	"os"
	"testing"

	"yascore/config"
	"yascore/internal/domain"
	"yascore/skills/mood"
)

func TestMain(m *testing.M) {
	cfg, err := config.LoadExample()
	if err != nil {
		panic(err)
	}
	domain.Configure(cfg.Core.Duck.Percent)
	ConfigureDialogue(cfg.Core.Dialogue.Keep, cfg.Core.Dialogue.ToolRounds, cfg.Core.Dialogue.TTL)
	ConfigureHold(cfg.Core.Hold.ResumeQuiet, cfg.Core.Hold.ResumeAfterQuestion)
	mood.Install(cfg.Skills.Mood.Tones)
	os.Exit(m.Run())
}

func exampleAssistant(t *testing.T) (string, []string) {
	t.Helper()
	cfg, err := config.LoadExample()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Core.Name, append([]string(nil), cfg.Core.Wake.Words...)
}
