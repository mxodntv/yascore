package repo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"yascore/internal/domain"
)

// CommandLog appends one JSON object per line. An empty path disables it.
// The file lock lets the core and the ear append without tearing a line.
type CommandLog struct {
	mu   sync.Mutex
	file *os.File
}

// NewCommandLog opens path, creating the parent directory and the file.
// An empty path returns a logger that records nothing.
func NewCommandLog(path string) (*CommandLog, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return &CommandLog{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("command log: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("command log: %w", err)
	}
	return &CommandLog{file: f}, nil
}

// Record writes one command. Concurrent calls, including from another process, do not tear a line.
func (l *CommandLog) Record(c domain.Command) {
	if l == nil {
		return
	}
	line, err := marshalCommand(c)
	if err != nil {
		log.Printf("command log: %v", err)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	fd := int(l.file.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		log.Printf("command log: %v", err)
		return
	}
	defer func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }()
	if _, err := l.file.Write(line); err != nil {
		log.Printf("command log: %v", err)
	}
}

// Close releases the file. A disabled logger returns nil.
func (l *CommandLog) Close() error {
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

func marshalCommand(c domain.Command) ([]byte, error) {
	c = prepareCommand(c)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func prepareCommand(c domain.Command) domain.Command {
	if strings.TrimSpace(c.TS) == "" {
		c.TS = time.Now().Format(time.RFC3339)
	} else {
		c.TS = clip(c.TS)
	}
	c.Started = clip(c.Started)
	c.Finished = clip(c.Finished)
	c.Source = clip(scrub(c.Source))
	c.Heard = clip(scrub(c.Heard))
	c.User = clip(scrub(c.User))
	c.Outcome = clip(scrub(c.Outcome))
	actions := make([]domain.CommandAction, len(c.Actions))
	for i, action := range c.Actions {
		actions[i] = domain.CommandAction{
			Name:   clip(scrub(action.Name)),
			By:     clip(scrub(action.By)),
			Start:  clip(action.Start),
			End:    clip(action.End),
			MS:     action.MS,
			Detail: clip(scrub(action.Detail)),
			Error:  clip(scrub(action.Error)),
		}
	}
	if actions == nil {
		actions = []domain.CommandAction{}
	}
	c.Actions = actions
	return c
}
