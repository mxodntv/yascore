package websearch

import (
	"context"
	"encoding/json"
	"time"

	"yascore/config"
	"yascore/internal/domain"
	"yascore/skills/protocol"
)

// Register connects this skill after the core server is already listening.
func Register(ctx context.Context, url string, search config.WebSearch) error {
	if search.TimeoutSec > 0 {
		searchClient.Timeout = time.Duration(search.TimeoutSec * float64(time.Second))
	}
	endpoint := search.Endpoint
	tool := domain.Tool{
		Name:        "web_search",
		Description: "Поиск в интернете: факты, новости, курсы, кто/когда/сколько.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []any{"query"},
		},
	}
	return protocol.Run(ctx, url, "websearch", []domain.Tool{tool},
		[]string{"найди", "погугли", "курс", "новост", "кто такой", "сколько", "поиск"},
		func(ctx context.Context, _ string, arguments string) (string, error) {
			var args struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal([]byte(arguments), &args)
			return Search(ctx, endpoint, args.Query)
		})
}
