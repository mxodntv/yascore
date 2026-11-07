package server

import (
	"os"
	"testing"

	"yascore/config"
	"yascore/internal/domain"
	"yascore/internal/usecase"
	"yascore/skills/mood"
)

func TestMain(m *testing.M) {
	cfg, err := config.LoadExample()
	if err != nil {
		panic(err)
	}
	domain.Configure(cfg.Core.Duck.Percent)
	usecase.ConfigureDialogue(cfg.Core.Dialogue.Keep, cfg.Core.Dialogue.ToolRounds, cfg.Core.Dialogue.TTL)
	usecase.ConfigureHold(cfg.Core.Hold.ResumeQuiet, cfg.Core.Hold.ResumeAfterQuestion)
	mood.Install(cfg.Skills.Mood.Tones)
	os.Exit(m.Run())
}
