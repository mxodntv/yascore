package repo

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Column is the only SSH path to the station player. Skills do not talk to it.
type Column struct {
	Host string
	Port int
	User string
	Bin  string

	// RunRemote is replaced in tests. Production uses SSH.
	RunRemote func(ctx context.Context, remote string) (string, error)
}

// NewColumn builds the column client from the loaded skill settings.
func NewColumn(host string, port int, user, bin string) *Column {
	return &Column{Host: host, Port: port, User: user, Bin: bin}
}

func (c *Column) run(ctx context.Context, remote string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("column unavailable")
	}
	if c.RunRemote != nil {
		return c.RunRemote(ctx, remote)
	}
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "ConnectTimeout=8",
		"-o", "StrictHostKeyChecking=accept-new",
		"-p", strconv.Itoa(c.Port),
		c.User+"@"+c.Host,
		remote,
	)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("музыка: %s", text)
	}
	if text == "" {
		return "ok", nil
	}
	return text, nil
}

func (c *Column) bin() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Bin)
}

func (c *Column) Play(ctx context.Context, phrase string) (string, error) {
	return c.run(ctx, shellQuote(c.bin())+" --json play "+shellQuote(phrase))
}

func (c *Column) Status(ctx context.Context) (string, error) {
	return c.run(ctx, shellQuote(c.bin())+" --json status")
}

func (c *Column) Resume(ctx context.Context) (string, error) {
	return c.run(ctx, shellQuote(c.bin())+" --json resume")
}

func (c *Column) Pause(ctx context.Context) (string, error) {
	return c.run(ctx, shellQuote(c.bin())+" pause")
}

func (c *Column) Stop(ctx context.Context) (string, error) {
	return c.run(ctx, shellQuote(c.bin())+" stop")
}

func (c *Column) Next(ctx context.Context) (string, error) {
	return c.run(ctx, shellQuote(c.bin())+" next")
}

func (c *Column) Previous(ctx context.Context) (string, error) {
	return c.run(ctx, shellQuote(c.bin())+" previous")
}

func (c *Column) Seek(ctx context.Context, seconds int) (string, error) {
	if seconds < 0 {
		return "", fmt.Errorf("отрицательная позиция")
	}
	return c.run(ctx, shellQuote(c.bin())+" seek "+strconv.Itoa(seconds))
}

func (c *Column) Search(ctx context.Context, query string) (string, error) {
	return c.run(ctx, shellQuote(c.bin())+" --json search "+shellQuote(query))
}

func (c *Column) Recommendations(ctx context.Context) (string, error) {
	return c.run(ctx, shellQuote(c.bin())+" --json recommendations")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
