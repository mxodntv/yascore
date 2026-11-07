package currency

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
	APIBase  string
	Database string
	Timeout  time.Duration
}

func (s Settings) normalized() Settings {
	s.APIBase = strings.TrimRight(strings.TrimSpace(s.APIBase), "/")
	s.Database = strings.TrimSpace(s.Database)
	return s
}

func settingsFrom(c config.Currency) Settings {
	return Settings{
		APIBase:  c.APIBase,
		Database: c.Database,
		Timeout:  time.Duration(c.TimeoutSec * float64(time.Second)),
	}
}

// Register connects the currency skill after the core server is already listening.
func Register(ctx context.Context, url string, c config.Currency) error {
	skill, err := New(settingsFrom(c))
	if err != nil {
		return err
	}
	defer skill.Close()
	return protocol.Run(ctx, url, "currency", []domain.Tool{tool()}, hints, skill.Handle)
}

func tool() domain.Tool {
	return domain.Tool{
		Name: "currency_rate",
		Description: "Курс валюты Центробанка в рублях. Не выдумывай курс и не спрашивай базовую валюту: база всегда рубль. " +
			"Пустой currency — сохранённая валюта. Если валюты нет, инструмент сам спросит, какую запомнить. " +
			"Первая названная валюта запоминается. Позже названная валюта — курс на один раз, сохранённая не меняется. " +
			"remember true — только когда пользователь просит запомнить или сменить валюту по умолчанию. " +
			"На вопрос, какая валюта по умолчанию, вызови с пустым currency и remember false: документ не меняется. " +
			"Ответ инструмента скажи как есть, коротко.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"currency": map[string]any{
					"type":        "string",
					"description": "Код (USD, EUR) или название (доллар, евро, юань, тенге). Пусто — сохранённая валюта. Не подставляй валюту, если её не назвали.",
				},
				"remember": map[string]any{
					"type":        "boolean",
					"description": "true только если пользователь просит запомнить или сменить валюту по умолчанию.",
				},
			},
		},
	}
}

var hints = []string{"валют", "доллар", "евро", "юан", "тенге", "курс"}
