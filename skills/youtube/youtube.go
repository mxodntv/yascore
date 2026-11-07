package youtube

import (
	"context"

	"yascore/internal/domain"
	"yascore/skills/protocol"
)

// Register connects this skill after the core server is already listening.
func Register(ctx context.Context, url string) error {
	tool := domain.Tool{
		Name:        "play_youtube",
		Description: "Включить видео на YouTube.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"query": map[string]any{"type": "string"}},
			"required":   []any{"query"},
		},
	}
	return protocol.Run(ctx, url, "youtube", []domain.Tool{tool}, []string{"youtube видео", "ютуб видео"}, protocol.NotReady("youtube"))
}
