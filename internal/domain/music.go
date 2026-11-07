package domain

// MusicIntent is what a music skill asks the core to do on the column.
// The skill never opens SSH; the core runs the player.
type MusicIntent struct {
	Do     string `json:"do"`
	Phrase string `json:"phrase,omitempty"`
	Query  string `json:"query,omitempty"`
	Action string `json:"action,omitempty"`
	Text   string `json:"text,omitempty"`
	Follow bool   `json:"follow,omitempty"`
	Index  *int   `json:"index,omitempty"`
	SeekMS int    `json:"seek_ms,omitempty"`
}

// Music intent names. Keep them short: they travel on the skill bus as JSON.
const (
	MusicPlay       = "play"
	MusicSearchPlay = "search_play"
	MusicResumeMix  = "resume_or_mix"
	MusicControl    = "control"
	MusicSay        = "say"
	MusicLike       = "like"
	MusicUnlike     = "unlike"
	MusicAdvance    = "advance"
)
