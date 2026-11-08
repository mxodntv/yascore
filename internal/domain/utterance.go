package domain

import "errors"

// ErrEmpty is returned when the utterance has no text after trim.
var ErrEmpty = errors.New("empty utterance")

// Transcript is recognized speech before wake-word stripping.
type Transcript struct {
	Text string
}

// SilentReply tells the column to say nothing and close the microphone.
const SilentReply = "[[varya:silent]]"

// EchoReply is a transcript that was only the assistant's own voice.
// The column says nothing and leaves the microphone as it is.
const EchoReply = "[[varya:echo]]"

// Reply is the assistant text. Quiet means the column does not speak it.
// Instant means a bare player command: no follow-up listen window after speech.
type Reply struct {
	Text    string
	Model   string
	Quiet   bool
	Instant bool
}

// Tool is a function the model may call. Parameters is a JSON Schema object.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolCall is one model request to run a tool.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Message is one turn sent to the LLM.
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}
