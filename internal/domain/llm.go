package domain

import "context"

// CompleteRequest is one LLM round. ToolChoice is "auto", "required", or "".
type CompleteRequest struct {
	System      string
	Messages    []Message
	Tools       []Tool
	ToolChoice  string
	Model       string
	MaxTokens   int
	Temperature float64
}

// Completion is the model output of one round.
type Completion struct {
	Text      string
	Model     string
	ToolCalls []ToolCall
}

// LLM talks to the configured model. Implementations live in repo.
type LLM interface {
	Complete(ctx context.Context, req CompleteRequest) (Completion, error)
}
