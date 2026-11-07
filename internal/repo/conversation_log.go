package repo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"yascore/internal/domain"
)

const maxLogRunes = 4000

var (
	bearerSecret = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{6,}`)
	assignSecret = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|secret|password|cookie|authorization)\b["']?\s*[:=]\s*["']?[^\s"',}]+`)
	skSecret     = regexp.MustCompile(`\bsk-[A-Za-z0-9]{8,}\b`)
)

// ConversationLog appends one JSON object per line. An empty path disables it.
type ConversationLog struct {
	mu   sync.Mutex
	file *os.File
}

// NewConversationLog opens path, creating the parent directory and the file.
// An empty path returns a logger that records nothing.
func NewConversationLog(path string) (*ConversationLog, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return &ConversationLog{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("conversation log: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("conversation log: %w", err)
	}
	return &ConversationLog{file: f}, nil
}

// Record writes one turn. Concurrent calls do not tear a line.
func (l *ConversationLog) Record(c domain.Conversation) {
	if l == nil {
		return
	}
	line, err := marshalConversation(c)
	if err != nil {
		log.Printf("conversation log: %v", err)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	if _, err := l.file.Write(line); err != nil {
		log.Printf("conversation log: %v", err)
	}
}

// Close releases the file. A disabled logger returns nil.
func (l *ConversationLog) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func marshalConversation(c domain.Conversation) ([]byte, error) {
	c = prepareConversation(c)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func prepareConversation(c domain.Conversation) domain.Conversation {
	if strings.TrimSpace(c.TS) == "" {
		c.TS = time.Now().Format(time.RFC3339)
	} else {
		c.TS = clip(c.TS)
	}
	c.User = clip(scrub(c.User))
	c.Reply = clip(scrub(c.Reply))
	c.Model = clip(scrub(c.Model))
	c.Error = clip(scrub(c.Error))
	c.Stack = clip(scrub(c.Stack))
	c.Started = clip(c.Started)
	c.Finished = clip(c.Finished)
	tools := make([]domain.ToolTrace, len(c.Tools))
	for i, tool := range c.Tools {
		tools[i] = domain.ToolTrace{
			Name:      clip(scrub(tool.Name)),
			Arguments: clip(scrub(tool.Arguments)),
			Result:    clip(scrub(tool.Result)),
		}
	}
	c.Tools = tools
	calls := make([]domain.CallSpan, len(c.Calls))
	for i, call := range c.Calls {
		calls[i] = domain.CallSpan{
			Name:  clip(scrub(call.Name)),
			By:    clip(scrub(call.By)),
			Start: clip(call.Start),
			End:   clip(call.End),
			MS:    call.MS,
			Error: clip(scrub(call.Error)),
		}
	}
	c.Calls = calls
	return c
}

func scrub(s string) string {
	s = bearerSecret.ReplaceAllString(s, "bearer [redacted]")
	s = assignSecret.ReplaceAllString(s, "${1}=[redacted]")
	s = skSecret.ReplaceAllString(s, "[redacted]")
	return s
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxLogRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLogRunes])
}
