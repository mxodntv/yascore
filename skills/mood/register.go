package mood

import (
	"context"
	"strings"

	"yascore/config"
	"yascore/internal/domain"
	"yascore/skills/protocol"
)

// Settings is the skill configuration. Tests pass a directory directly.
type Settings struct {
	Database string
}

func (s Settings) normalized() Settings {
	s.Database = strings.TrimSpace(s.Database)
	return s
}

// Register connects the mood skill after the core server is already listening.
// It is an ordinary skill: it does not play audio.
func Register(ctx context.Context, url string, moodCfg config.Mood) error {
	Install(moodCfg.Tones)
	skill, err := New(Settings{Database: moodCfg.Database})
	if err != nil {
		return err
	}
	defer skill.Close()
	return protocol.Run(ctx, url, "mood", []domain.Tool{tool()}, hints, skill.Handle)
}

func tool() domain.Tool {
	return domain.Tool{
		Name: "set_mood",
		Description: "Настроение. Когда просят сменить тон — «стань злой», «стань доброжелательной», «стань милой», «стань весёлой», «стань спокойной», «доброй будь» — передай mood как сказали, без имени. " +
			"Когда спрашивают, какое настроение или какая ты сейчас, mood пустой: документ не меняется. " +
			"Не выдумывай настроение. Ответ скажи как есть, одним коротким предложением, без списков.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mood": map[string]any{
					"type":        "string",
					"description": "Как просят стать: злой, доброжелательной, милой, весёлой, спокойной. Пусто — сказать текущее настроение.",
				},
			},
		},
	}
}

var hints = []string{
	"настроен",
	"стань ",
	"доброй",
	"доброжелатель",
	"злой",
	"злая",
	"милой",
	"милая",
	"весёл",
	"весел",
	"спокойн",
	"какая ты",
}
