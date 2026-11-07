package vkvideo

import (
	"context"

	"yascore/internal/domain"
	"yascore/skills/protocol"
)

// Register connects this skill after the core server is already listening.
func Register(ctx context.Context, url string) error {
	tool := domain.Tool{
		Name:        "play_vk_video",
		Description: "Включить видео VK.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"query": map[string]any{"type": "string"}},
			"required":   []any{"query"},
		},
	}
	return protocol.Run(ctx, url, "vkvideo", []domain.Tool{tool}, []string{"вк видео", "vk video"}, protocol.NotReady("vkvideo"))
}
