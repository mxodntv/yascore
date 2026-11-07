package domain

import "context"

// Message is the skill-bus envelope. Skills and the hub share this shape.
type BusMessage struct {
	Type      string   `json:"type"`
	Name      string   `json:"name,omitempty"`
	Tools     []Tool   `json:"tools,omitempty"`
	Hints     []string `json:"hints,omitempty"`
	Sound     bool     `json:"sound,omitempty"`
	Media     bool     `json:"media,omitempty"`
	ID        string   `json:"id,omitempty"`
	Arguments string   `json:"arguments,omitempty"`
	Content   string   `json:"content,omitempty"`
}

// SkillBus is the set of skills currently connected.
type SkillBus interface {
	Tools(ctx context.Context) []Tool
	Hints(ctx context.Context) []string
	Invoke(ctx context.Context, call ToolCall) (string, error)
	Count() int
}

// SkillConn is the byte pipe a skill uses. It lives here so server and repo
// can share it without importing each other.
type SkillConn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, p []byte) error
	Close() error
}

// SkillAcceptor owns one skill socket until it drops.
type SkillAcceptor interface {
	Accept(ctx context.Context, conn SkillConn) error
}
