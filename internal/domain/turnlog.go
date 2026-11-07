package domain

// ToolTrace is one skill call made while handling a turn.
type ToolTrace struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
}

// CallSpan is one outbound step while an utterance was handled.
// Start and End are RFC3339Nano. MS is the elapsed time in milliseconds.
// Error is set when that step failed; the turn still keeps Conversation.Error.
type CallSpan struct {
	Name  string `json:"name"`
	By    string `json:"by"`
	Start string `json:"start"`
	End   string `json:"end"`
	MS    int64  `json:"ms"`
	Error string `json:"error,omitempty"`
}

// Conversation is one handled utterance. The journal writes it as a single JSON line.
type Conversation struct {
	TS       string      `json:"ts"`
	User     string      `json:"user"`
	Reply    string      `json:"reply"`
	Quiet    bool        `json:"quiet"`
	Model    string      `json:"model"`
	Error    string      `json:"error"`
	Stack    string      `json:"stack"`
	Tools    []ToolTrace `json:"tools"`
	Started  string      `json:"started"`
	Finished string      `json:"finished"`
	MS       int64       `json:"ms"`
	Calls    []CallSpan  `json:"calls"`
}

// TurnLogger records a handled turn. Implementations live outside domain.
// A nil logger discards the turn.
type TurnLogger interface {
	Record(Conversation)
}
