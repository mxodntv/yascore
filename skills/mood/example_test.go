package mood

import (
	"os"
	"testing"

	"yascore/config"
)

func TestMain(m *testing.M) {
	cfg, err := config.LoadExample()
	if err != nil {
		panic(err)
	}
	Install(cfg.Skills.Mood.Tones)
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
