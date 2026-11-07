package usecase

import (
	"context"
	"errors"
	"runtime/debug"
	"strings"
	"time"

	"yascore/internal/domain"
)

func (u *Utterance) record(ctx context.Context, c domain.Conversation) {
	if u == nil || u.log == nil {
		return
	}
	if c.Model == "" {
		c.Model = u.model
	}
	if c.Tools == nil {
		c.Tools = []domain.ToolTrace{}
	}
	if strings.TrimSpace(c.Stack) == "" {
		if c.Error != "" {
			c.Stack = string(debug.Stack())
		} else {
			c.Stack = branchOf(c.Tools)
		}
	}
	applyCommand(ctx, c)
	if tr := traceFrom(ctx); tr != nil {
		tr.stash(c)
		return
	}
	now := time.Now()
	if c.Started == "" {
		c.Started = now.Format(time.RFC3339Nano)
	}
	if c.Finished == "" {
		c.Finished = c.Started
	}
	if c.Calls == nil {
		c.Calls = []domain.CallSpan{}
	}
	u.log.Record(c)
}

func branchOf(tools []domain.ToolTrace) string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "plain model reply"
	}
	return "tools: " + strings.Join(names, ", ")
}

// stackFrom uses a stack already carried by err, otherwise the current goroutine.
func stackFrom(err error) string {
	for err != nil {
		if s, ok := err.(interface{ Stack() string }); ok {
			if st := strings.TrimSpace(s.Stack()); st != "" {
				return st
			}
		}
		if s, ok := err.(interface{ StackTrace() string }); ok {
			if st := strings.TrimSpace(s.StackTrace()); st != "" {
				return st
			}
		}
		err = errors.Unwrap(err)
	}
	return string(debug.Stack())
}
