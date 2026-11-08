package musicyoutube

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
	UseService(cfg.Skills.MusicYouTube.ServiceURL)
	os.Exit(m.Run())
}
