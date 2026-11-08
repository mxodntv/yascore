package musicyoutube

import (
	"encoding/json"
	"strings"
)

type foundTrack struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Artists string `json:"artists"`
}

// pickTrackID chooses the search hit that shares the most words with the request
// and is not a cover or 8-bit version.
func pickTrackID(searchJSON, query string) string {
	var parsed struct {
		Tracks []foundTrack `json:"tracks"`
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

func summarizeStatus(statusJSON string) string {
	var parsed struct {
		State struct {
			Status string       `json:"status"`
			Error  string       `json:"error"`
			Index  int          `json:"index"`
			Queue  []foundTrack `json:"queue"`
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

func vagueMusic(query, kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "any", "mix":
		return cleanQuery(query) == "" || musicFiller(cleanQuery(query))
	case "likes":
		return false
	}
	return musicFiller(cleanQuery(query))
}

func musicFiller(query string) bool {
	switch strings.ToLower(strings.TrimSpace(query)) {
	case "", "музыка", "музыку", "музыке", "music",
		"что-нибудь", "что нибудь", "чтонибудь", "любую", "любое", "любая":
		return true
	default:
		return false
	}
}

// nextMusicAction decides a bare «play music» request.
// resume continues a stopped or paused queue, already means sound is on, mix starts a personal collection.
func nextMusicAction(statusJSON string) string {
	var parsed struct {
		State struct {
			Status string       `json:"status"`
			Queue  []foundTrack `json:"queue"`
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

var serviceBase string

// UseService sets the music site base used to build watch and playlist links.
func UseService(raw string) {
	serviceBase = strings.TrimRight(strings.TrimSpace(raw), "/")
}

func playlistPhrase(query string) string {
	q := strings.TrimSpace(query)
	if strings.Contains(q, "list=") || strings.HasPrefix(q, "http://") || strings.HasPrefix(q, "https://") {
		return q
	}
	if strings.HasPrefix(q, "PL") || strings.HasPrefix(q, "RD") {
		return serviceBase + "/playlist?list=" + q
	}
	return q
}

func cleanQuery(query string) string {
	q := strings.TrimSpace(query)
	lower := strings.ToLower(q)
	for _, prefix := range []string{
		"включи ", "поставь ", "играй ", "запусти ",
		"песню ", "песня ", "трек ", "плейлист ", "альбом ",
		"исполнителя ", "исполнитель ", "группу ", "группа ",
	} {
		if strings.HasPrefix(lower, prefix) {
			q = strings.TrimSpace(q[len(prefix):])
			lower = strings.ToLower(q)
		}
	}
	return q
}

func words(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	var out []string
	for _, field := range fields {
		field = strings.Trim(field, ".,!?;:\"'")
		if len([]rune(field)) < 3 {
			continue
		}
		out = append(out, field)
	}
	return out
}
