package domain

import "time"

// CommandAction is one step that actually ran while a command was handled.
// Start and End are RFC3339Nano. MS is the elapsed time in milliseconds.
// Error is set when that step failed; the step is still kept.
type CommandAction struct {
	Name   string `json:"name"`
	By     string `json:"by"`
	Start  string `json:"start"`
	End    string `json:"end"`
	MS     int64  `json:"ms"`
	Detail string `json:"detail"`
	Error  string `json:"error,omitempty"`
}

// Command is one finished command, or one heard phrase that was ignored.
// The journal writes it as a single JSON line when the turn finishes.
type Command struct {
	TS       string          `json:"ts"`
	Started  string          `json:"started"`
	Finished string          `json:"finished"`
	MS       int64           `json:"ms"`
	Source   string          `json:"source"`
	Heard    string          `json:"heard"`
	User     string          `json:"user"`
	Outcome  string          `json:"outcome"`
	Actions  []CommandAction `json:"actions"`
}

// CommandLogger appends a finished command. A nil logger discards it.
type CommandLogger interface {
	Record(Command)
}

// Span formats both instants and returns the milliseconds between them.
func Span(start, end time.Time) (string, string, int64) {
	if end.Before(start) {
		end = start
	}
	startS := start.Format(time.RFC3339Nano)
	endS := end.Format(time.RFC3339Nano)
	s, errS := time.Parse(time.RFC3339Nano, startS)
	e, errE := time.Parse(time.RFC3339Nano, endS)
	if errS != nil || errE != nil {
		return startS, endS, end.Sub(start).Milliseconds()
	}
	return startS, endS, e.Sub(s).Milliseconds()
}

// NewAction builds one step. A nil error leaves Error empty.
func NewAction(name, by, detail string, start, end time.Time, err error) CommandAction {
	startS, endS, ms := Span(start, end)
	action := CommandAction{
		Name: name, By: by, Detail: detail,
		Start: startS, End: endS, MS: ms,
	}
	if err != nil {
		action.Error = err.Error()
	}
	return action
}
