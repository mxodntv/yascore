package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"yascore/internal/domain"
)

// Ollama is an OpenAI-compatible chat client. ToolChoice is always a string.
type Ollama struct {
	baseURL string
	client  *http.Client
	model   string
}

// NewOllama builds a client. baseURL includes the /v1 prefix.
func NewOllama(baseURL, model string, timeout time.Duration) *Ollama {
	return &Ollama{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (o *Ollama) Complete(ctx context.Context, req domain.CompleteRequest) (domain.Completion, error) {
	model := req.Model
	if model == "" {
		model = o.model
	}
	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		item := map[string]any{"role": m.Role, "content": m.Content}
		if m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, c := range m.ToolCalls {
				calls = append(calls, map[string]any{
					"id":   c.ID,
					"type": "function",
					"function": map[string]any{
						"name":      c.Name,
						"arguments": c.Arguments,
					},
				})
			}
			item["tool_calls"] = calls
		}
		msgs = append(msgs, item)
	}
	body := map[string]any{
		"model":       model,
		"messages":    msgs,
		"stream":      false,
		"temperature": req.Temperature,
		"max_tokens":  req.MaxTokens,
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			params := tool.Parameters
			if params == nil {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  params,
				},
			})
		}
		body["tools"] = tools
		choice := req.ToolChoice
		if choice == "" {
			choice = "auto"
		}
		body["tool_choice"] = choice
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return domain.Completion{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return domain.Completion{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return domain.Completion{}, fmt.Errorf("timeout: %w", err)
		}
		return domain.Completion{}, err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 500 {
		return domain.Completion{}, fmt.Errorf("llm_5xx: %s", bytes.TrimSpace(payload))
	}
	if resp.StatusCode >= 400 {
		return domain.Completion{}, fmt.Errorf("llm_4xx: %s", bytes.TrimSpace(payload))
	}
	return decodeCompletion(payload)
}

func decodeCompletion(payload []byte) (domain.Completion, error) {
	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   json.RawMessage `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return domain.Completion{}, fmt.Errorf("llm decode: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return domain.Completion{}, fmt.Errorf("llm_5xx: empty choices")
	}
	msg := parsed.Choices[0].Message
	out := domain.Completion{Model: parsed.Model, Text: rawString(msg.Content)}
	for _, call := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, domain.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: rawString(call.Function.Arguments),
		})
	}
	return out, nil
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
