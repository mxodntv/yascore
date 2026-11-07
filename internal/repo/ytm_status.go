package repo

import (
	"encoding/json"
	"strconv"
	"strings"
)

type ytmTrack struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Artists string `json:"artists"`
}

type ytmSpot struct {
	MS      int
	Raw     string
	Seconds bool
}

func spotFromStatus(statusJSON string) ytmSpot {
	var parsed struct {
		State struct {
			PositionMS json.RawMessage `json:"position_ms"`
			Position   json.RawMessage `json:"position"`
			Progress   json.RawMessage `json:"progress"`
			Elapsed    json.RawMessage `json:"elapsed"`
			Index      *int            `json:"index"`
		} `json:"state"`
	}
	if json.Unmarshal([]byte(statusJSON), &parsed) != nil {
		return ytmSpot{MS: -1}
	}
	if n, ok := jsonInt(parsed.State.PositionMS); ok {
		return ytmSpot{MS: n, Raw: strconv.Itoa(n) + "ms", Seconds: true}
	}
	if n, ok := jsonInt(parsed.State.Progress); ok {
		return ytmSpot{MS: n, Raw: strconv.Itoa(n) + "ms", Seconds: true}
	}
	if f, ok := jsonFloat(parsed.State.Elapsed); ok {
		ms := int(f * 1000)
		if ms < 0 {
			ms = 0
		}
		return ytmSpot{MS: ms, Raw: strconv.FormatFloat(f, 'f', -1, 64) + "s", Seconds: true}
	}
	if text := jsonText(parsed.State.Position); text != "" {
		return ytmSpot{MS: -1, Raw: text}
	}
	if parsed.State.Index != nil {
		return ytmSpot{MS: -1, Raw: "index:" + strconv.Itoa(*parsed.State.Index)}
	}
	return ytmSpot{MS: -1}
}

func jsonInt(raw json.RawMessage) (int, bool) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int(f), true
	}
	return 0, false
}

func jsonFloat(raw json.RawMessage) (float64, bool) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f, true
	}
	return 0, false
}

func jsonText(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	return strings.Trim(string(raw), `"`)
}

func trackFromStatus(statusJSON string) (ytmTrack, bool) {
	var parsed struct {
		State struct {
			Index int        `json:"index"`
			Queue []ytmTrack `json:"queue"`
		} `json:"state"`
	}
	if json.Unmarshal([]byte(statusJSON), &parsed) != nil || len(parsed.State.Queue) == 0 {
		return ytmTrack{}, false
	}
	i := parsed.State.Index
	if i < 0 || i >= len(parsed.State.Queue) {
		i = 0
	}
	t := parsed.State.Queue[i]
	if strings.TrimSpace(t.Title) == "" && strings.TrimSpace(t.ID) == "" {
		return ytmTrack{}, false
	}
	return t, true
}

func summarizeStatus(statusJSON string) string {
	var parsed struct {
		State struct {
			Status string     `json:"status"`
			Error  string     `json:"error"`
			Index  int        `json:"index"`
			Queue  []ytmTrack `json:"queue"`
		} `json:"state"`
	}
	if json.Unmarshal([]byte(statusJSON), &parsed) != nil || parsed.State.Status == "" {
		return "не играет: плеер не ответил"
	}
	title := ""
	if n := len(parsed.State.Queue); n > 0 {
		i := parsed.State.Index
		if i < 0 || i >= n {
			i = 0
		}
		cur := parsed.State.Queue[i]
		title = strings.Trim(strings.TrimSpace(cur.Title)+" — "+strings.TrimSpace(cur.Artists), " —")
	}
	switch parsed.State.Status {
	case "playing", "buffering":
		if title == "" {
			return "включаю"
		}
		return "играет " + title
	case "error":
		msg := strings.TrimSpace(parsed.State.Error)
		if msg == "" {
			msg = "ошибка плеера"
		}
		if title == "" {
			return "не играет: " + msg
		}
		return "не играет " + title + ": " + msg
	default:
		if title == "" {
			return "плеер: " + parsed.State.Status
		}
		return "плеер: " + parsed.State.Status + ", " + title
	}
}

func nextMusicAction(statusJSON string) string {
	var parsed struct {
		State struct {
			Status string     `json:"status"`
			Queue  []ytmTrack `json:"queue"`
		} `json:"state"`
	}
	if json.Unmarshal([]byte(statusJSON), &parsed) != nil {
		return "mix"
	}
	switch parsed.State.Status {
	case "playing", "buffering":
		if len(parsed.State.Queue) > 0 {
			return "already"
		}
	case "stopped", "paused", "error":
		if len(parsed.State.Queue) > 0 {
			return "resume"
		}
	}
	return "mix"
}

func soundActive(statusJSON string) bool {
	var parsed struct {
		State struct {
			Status string `json:"status"`
		} `json:"state"`
	}
	if json.Unmarshal([]byte(statusJSON), &parsed) != nil {
		return false
	}
	return parsed.State.Status == "playing" || parsed.State.Status == "buffering"
}

func readyLine(statusJSON string) (string, bool) {
	line := summarizeStatus(statusJSON)
	if strings.HasPrefix(line, "играет ") || line == "включаю" {
		return line, true
	}
	return "", false
}

func libraryUnavailable(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "подключи youtube") || strings.Contains(lower, "личную библиотеку")
}

func playerStatus(statusJSON string) string {
	var parsed struct {
		State struct {
			Status string `json:"status"`
		} `json:"state"`
	}
	if json.Unmarshal([]byte(statusJSON), &parsed) != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.State.Status))
}

func pickTrackID(searchJSON, query string) string {
	var parsed struct {
		Tracks []ytmTrack `json:"tracks"`
	}
	if json.Unmarshal([]byte(searchJSON), &parsed) != nil || len(parsed.Tracks) == 0 {
		return ""
	}
	tokens := words(query)
	bestID := parsed.Tracks[0].ID
	best := -100
	for _, track := range parsed.Tracks {
		if track.ID == "" {
			continue
		}
		score := 0
		blob := strings.ToLower(track.Title + " " + track.Artists)
		for _, token := range tokens {
			if strings.Contains(blob, token) {
				score += 2
			}
		}
		title := strings.ToLower(track.Title)
		for _, bad := range []string{"8-bit", "instrumental", "karaoke", "cover", "originally performed", "as made famous"} {
			if strings.Contains(title, bad) {
				score -= 4
			}
		}
		if score > best {
			best = score
			bestID = track.ID
		}
	}
	return bestID
}

func words(query string) []string {
	var out []string
	for _, p := range strings.Fields(strings.ToLower(query)) {
		p = strings.Trim(p, ".,!?;:«»\"'")
		if len(p) > 1 {
			out = append(out, p)
		}
	}
	return out
}

func needsSeek(currentMS, positionMS int) bool {
	if positionMS <= 0 {
		return false
	}
	if currentMS < 0 {
		return true
	}
	delta := currentMS - positionMS
	if delta < 0 {
		delta = -delta
	}
	return delta > 2000
}
