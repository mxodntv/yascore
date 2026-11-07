package youtubemusic

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
	UseService(cfg.Skills.YouTubeMusic.ServiceURL)
	os.Exit(m.Run())
}
