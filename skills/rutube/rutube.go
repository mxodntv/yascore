package rutube

import (
	"context"

	"yascore/internal/domain"
	"yascore/skills/protocol"
)

// Register connects this skill after the core server is already listening.
func Register(ctx context.Context, url string) error {
	tool := domain.Tool{
		Name:        "play_rutube",
		Description: "Включить видео на Rutube.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"query": map[string]any{"type": "string"}},
			"required":   []any{"query"},
		},
	}
	return protocol.Run(ctx, url, "rutube", []domain.Tool{tool}, []string{"рутуб", "rutube"}, protocol.NotReady("rutube"))
}
