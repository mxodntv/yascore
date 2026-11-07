package smarthome

import (
	"context"

	"yascore/internal/domain"
	"yascore/skills/protocol"
)

// Register connects this skill after the core server is already listening.
func Register(ctx context.Context, url string) error {
	tool := domain.Tool{
		Name:        "home",
		Description: "Управление умным домом через Home Assistant.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
			},
			"required": []any{"command"},
		},
	}
	return protocol.Run(ctx, url, "smarthome", []domain.Tool{tool}, []string{"свет", "дом", "шторы"}, protocol.NotReady("smarthome"))
}
