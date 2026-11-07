package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coder/websocket"

	"yascore/internal/domain"
)

// Handler runs one tool call for a skill process.
type Handler func(ctx context.Context, name, arguments string) (string, error)

// Level reports or sets this skill's volume. An empty argument asks for the current level as JSON.
type Level func(ctx context.Context, arguments string) (string, error)

// Run dials the core, registers, and answers invoke messages until the socket closes.
func Run(ctx context.Context, url, name string, tools []domain.Tool, hints []string, handle Handler) error {
	return run(ctx, url, name, tools, hints, false, false, handle, nil)
}

// RunSound is Run for a skill that plays audio and can be ducked while the assistant speaks.
func RunSound(ctx context.Context, url, name string, tools []domain.Tool, hints []string, handle Handler, level Level) error {
	return run(ctx, url, name, tools, hints, true, false, handle, level)
}

// RunMedia is RunSound for a skill that owns playback and can pause and resume it.
func RunMedia(ctx context.Context, url, name string, tools []domain.Tool, hints []string, handle Handler, level Level) error {
	return run(ctx, url, name, tools, hints, true, true, handle, level)
}

func run(ctx context.Context, url, name string, tools []domain.Tool, hints []string, sound, media bool, handle Handler, level Level) error {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	reg, err := json.Marshal(domain.BusMessage{Type: "register", Name: name, Tools: tools, Hints: hints, Sound: sound, Media: media})
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageText, reg); err != nil {
		return err
	}
	_, ackRaw, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	var ack domain.BusMessage
	if err := json.Unmarshal(ackRaw, &ack); err != nil {
		return err
	}
	if ack.Type != "registered" {
		return fmt.Errorf("register rejected: %s", ack.Content)
	}
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var msg domain.BusMessage
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		if msg.Type == "pause" || msg.Type == "resume" {
			content := "ok"
			if handle != nil {
				var callErr error
				content, callErr = handle(ctx, msg.Type, msg.Arguments)
				if callErr != nil {
					content = callErr.Error()
				}
			}
			if msg.Type == "pause" {
				switch strings.TrimSpace(content) {
				case "", "ok", domain.SilentReply:
					content = "ok"
				}
			}
			out, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: content})
			if err := conn.Write(ctx, websocket.MessageText, out); err != nil {
				return err
			}
			continue
		}
		if msg.Type == "level" {
			if level == nil {
				continue
			}
			content, callErr := level(ctx, msg.Arguments)
			if callErr != nil {
				content = callErr.Error()
			}
			out, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: content})
			if err := conn.Write(ctx, websocket.MessageText, out); err != nil {
				return err
			}
			continue
		}
		if msg.Type != "invoke" {
			continue
		}
		content, callErr := handle(ctx, msg.Name, msg.Arguments)
		if callErr != nil {
			content = callErr.Error()
		}
		out, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: content})
		if err := conn.Write(ctx, websocket.MessageText, out); err != nil {
			return err
		}
	}
}

// NotReady is a handler for skills that are wired but not implemented yet.
func NotReady(name string) Handler {
	return func(context.Context, string, string) (string, error) {
		return "скилл " + name + " ещё не подключён", nil
	}
}
