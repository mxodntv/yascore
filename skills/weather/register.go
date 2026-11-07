package weather

import (
	"context"
	"strings"
	"time"

	"yascore/config"
	"yascore/internal/domain"
	"yascore/skills/protocol"
)

// Settings is the skill configuration. The composition root publishes the yaml
// section through the environment; tests pass a Settings value directly.
type Settings struct {
	Provider    string
	APIBase     string
	GeocodeBase string
	Database    string
	Timeout     time.Duration
}

func (s Settings) normalized() Settings {
	s.Provider = strings.TrimSpace(s.Provider)
	s.APIBase = strings.TrimSpace(s.APIBase)
	s.GeocodeBase = strings.TrimSpace(s.GeocodeBase)
	s.Database = strings.TrimSpace(s.Database)
	return s
}

func settingsFrom(w config.Weather) Settings {
	return Settings{
		Provider:    w.Provider,
		APIBase:     w.APIBase,
		GeocodeBase: w.GeocodeBase,
		Database:    w.Database,
		Timeout:     time.Duration(w.TimeoutSec * float64(time.Second)),
	}
}

// Register connects Погода after the core server is already listening.
func Register(ctx context.Context, url string, w config.Weather) error {
	skill, err := New(settingsFrom(w))
	if err != nil {
		return err
	}
	defer skill.Close()
	return protocol.Run(ctx, url, "weather", []domain.Tool{tool()}, hints, skill.Handle)
}

func tool() domain.Tool {
	return domain.Tool{
		Name: "weather",
		Description: "Погода. Текущая погода. Не выдумывай страну и город. " +
			"Пустой city — сохранённое место. Если места нет, инструмент сам спросит страну и город. " +
			"country вместе с city запоминает это место как город по умолчанию. " +
			"Только city — погода в названном городе на один раз, сохранённое место не меняется. " +
			"Удачный ответ: первая строка — фраза о погоде, вторая asks_today — сколько раз погоду спросили сегодня.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "Город. Пусто — взять сохранённое место. Не подставляй город, если его не назвали.",
				},
				"country": map[string]any{
					"type":        "string",
					"description": "Страна. Вместе с городом сохраняется как место по умолчанию. Пусто — не менять сохранённый город.",
				},
			},
		},
	}
}

var hints = []string{"погод", "температур", "дождь", "градус"}
