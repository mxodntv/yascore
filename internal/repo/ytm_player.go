package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"yascore/config"
	"yascore/internal/domain"
)

// IntentAsk asks a media skill for the next intent without the skill touching the column.
type IntentAsk func(ctx context.Context, name, arguments string) (string, error)

// YTMPlayer is the core's column music player. Skills only return intents.
type YTMPlayer struct {
	col     *Column
	ask     IntentAsk
	service string

	mu      sync.Mutex
	lastPos int
	follow  ytmFollow
}

type ytmFollow struct {
	armed   bool
	heard   bool
	busy    bool
	tries   int
	gen     uint64
	current ytmTrack
}

// NewYTMPlayer builds the player. service is the music site base URL.
// ask may be nil until the hub wires it.
func NewYTMPlayer(col *Column, service string) *YTMPlayer {
	return &YTMPlayer{col: col, service: strings.TrimRight(strings.TrimSpace(service), "/"), lastPos: -1}
}

// UseAsk sets how the player asks the music skill for intents (advance, like).
func (p *YTMPlayer) UseAsk(ask IntentAsk) {
	if p == nil {
		return
	}
	p.ask = ask
}

// StartWatch polls the column while a queued track is armed.
func (p *YTMPlayer) StartWatch(ctx context.Context) {
	if p == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !p.followArmed() {
				continue
			}
			gen := p.followGen()
			raw, err := p.col.Status(ctx)
			if err != nil || ctx.Err() != nil || p.followGen() != gen {
				continue
			}
			_ = p.observe(ctx, raw)
		}
	}
}

// PausePlaying pauses a playing track and returns its millisecond spot.
// If the column is already quiet (stopped or paused), it returns -1 and does
// not arm an auto-resume: a later chat must not bring a stopped track back.
func (p *YTMPlayer) PausePlaying(ctx context.Context) (int, error) {
	if p == nil || p.col == nil {
		return -1, fmt.Errorf("player unavailable")
	}
	raw, err := p.col.Status(ctx)
	if err != nil {
		return -1, err
	}
	if !soundActive(raw) {
		p.mu.Lock()
		p.lastPos = -1
		p.mu.Unlock()
		return -1, nil
	}
	sp := spotFromStatus(raw)
	if !sp.Seconds && sp.Raw != "" {
		config.Debug("player position is not seconds", "raw", sp.Raw)
	}
	if _, err := p.col.Pause(ctx); err != nil {
		return sp.MS, err
	}
	p.mu.Lock()
	p.lastPos = sp.MS
	p.mu.Unlock()
	return sp.MS, nil
}

// ResumePlaying continues at positionMS. Negative uses the last pause spot.
func (p *YTMPlayer) ResumePlaying(ctx context.Context, positionMS int) error {
	if p == nil || p.col == nil {
		return fmt.Errorf("player unavailable")
	}
	if positionMS < 0 {
		p.mu.Lock()
		positionMS = p.lastPos
		p.mu.Unlock()
	}
	raw, err := p.col.Status(ctx)
	if err != nil {
		return err
	}
	switch nextMusicAction(raw) {
	case "resume":
		cur := spotFromStatus(raw)
		if _, err := p.col.Resume(ctx); err != nil {
			return err
		}
		return p.seekAfterResume(ctx, cur.MS, positionMS)
	case "already":
		return nil
	default:
		return fmt.Errorf("нечего продолжать")
	}
}

func (p *YTMPlayer) seekAfterResume(ctx context.Context, currentMS, positionMS int) error {
	if !needsSeek(currentMS, positionMS) {
		return nil
	}
	seconds := positionMS / 1000
	if seconds <= 0 {
		return nil
	}
	_, err := p.col.Seek(ctx, seconds)
	return err
}

// Playing reports whether the column is making music sound.
func (p *YTMPlayer) Playing(ctx context.Context) bool {
	if p == nil || p.col == nil {
		return false
	}
	raw, err := p.col.Status(ctx)
	if err != nil {
		return false
	}
	return soundActive(raw)
}

// Control runs a bare player action on the column.
func (p *YTMPlayer) Control(ctx context.Context, action string) (string, error) {
	if p == nil || p.col == nil {
		return "", fmt.Errorf("player unavailable")
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "status":
		raw, err := p.col.Status(ctx)
		if err != nil {
			return "", err
		}
		return summarizeStatus(raw), nil
	case "stop":
		p.disarm()
		if _, err := p.col.Stop(ctx); err != nil {
			return "", err
		}
		return domain.SilentReply, nil
	case "pause":
		if _, err := p.PausePlaying(ctx); err != nil {
			return "", err
		}
		return domain.SilentReply, nil
	case "resume", "play":
		if err := p.ResumePlaying(ctx, -1); err != nil {
			return "", err
		}
		return domain.SilentReply, nil
	case "next":
		if _, err := p.col.Next(ctx); err != nil {
			return "", err
		}
		return domain.SilentReply, nil
	case "previous", "prev":
		if _, err := p.col.Previous(ctx); err != nil {
			return "", err
		}
		return domain.SilentReply, nil
	default:
		return "", fmt.Errorf("неизвестная команда плеера: %s", action)
	}
}

// ApplyIntent runs one skill intent on the column.
func (p *YTMPlayer) ApplyIntent(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return domain.SilentReply, nil
	}
	var intent domain.MusicIntent
	if json.Unmarshal([]byte(raw), &intent) != nil || intent.Do == "" {
		// Plain speech from the skill (likes phrases, errors).
		return raw, nil
	}
	return p.apply(ctx, intent)
}

func (p *YTMPlayer) apply(ctx context.Context, intent domain.MusicIntent) (string, error) {
	switch intent.Do {
	case domain.MusicSay:
		if intent.Text == "" {
			return domain.SilentReply, nil
		}
		return intent.Text, nil
	case domain.MusicPlay:
		return p.startPhrase(ctx, intent.Phrase, intent.Follow)
	case domain.MusicSearchPlay:
		phrase, err := p.searchPhrase(ctx, intent.Query)
		if err != nil {
			return "", err
		}
		return p.startPhrase(ctx, phrase, intent.Follow)
	case domain.MusicResumeMix:
		return p.resumeOrMix(ctx)
	case domain.MusicControl:
		return p.Control(ctx, intent.Action)
	case domain.MusicLike, domain.MusicUnlike:
		return p.likeOrUnlike(ctx, intent.Do)
	case domain.MusicAdvance:
		return p.ApplyIntent(ctx, rawFromAsk(ctx, p, "music_control", `{"action":"advance"}`))
	default:
		return "", fmt.Errorf("unknown music intent %s", intent.Do)
	}
}

func rawFromAsk(ctx context.Context, p *YTMPlayer, name, args string) string {
	if p == nil || p.ask == nil {
		return ""
	}
	raw, err := p.ask(ctx, name, args)
	if err != nil {
		return ""
	}
	return raw
}

func (p *YTMPlayer) likeOrUnlike(ctx context.Context, do string) (string, error) {
	if p.ask == nil {
		return "", fmt.Errorf("music skill unavailable")
	}
	action := "like"
	if do == domain.MusicUnlike {
		action = "unlike"
	}
	args := map[string]any{"action": action}
	if raw, err := p.col.Status(ctx); err == nil {
		if track, ok := trackFromStatus(raw); ok {
			args["title"] = track.Title
			args["artists"] = track.Artists
			args["id"] = track.ID
		}
	}
	body, _ := json.Marshal(args)
	raw, err := p.ask(ctx, "music_control", string(body))
	if err != nil {
		return "", err
	}
	return p.ApplyIntent(ctx, raw)
}

func (p *YTMPlayer) searchPhrase(ctx context.Context, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("не поняла, какую песню включить")
	}
	raw, err := p.col.Search(ctx, query)
	if err != nil {
		return query, nil
	}
	id := pickTrackID(raw, query)
	if id == "" {
		return query, nil
	}
	return p.service + "/watch?v=" + id, nil
}

func (p *YTMPlayer) startPhrase(ctx context.Context, phrase string, follow bool) (string, error) {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return "", fmt.Errorf("не поняла, что включить")
	}
	var last string
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := p.col.Play(ctx, phrase)
		if err != nil {
			return "", err
		}
		if text, ok := readyLine(raw); ok {
			if follow {
				p.armFromStatus(raw)
			}
			return text, nil
		}
		text, err := p.afterPlay(ctx)
		if err != nil {
			return "", err
		}
		last = text
		if !strings.HasPrefix(text, "не играет") {
			if follow {
				if status, stErr := p.col.Status(ctx); stErr == nil {
					p.armFromStatus(status)
				}
			}
			return text, nil
		}
	}
	return last, nil
}

func (p *YTMPlayer) afterPlay(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(3 * time.Second):
	}
	status, err := p.col.Status(ctx)
	if err != nil {
		return "", err
	}
	return summarizeStatus(status), nil
}

func (p *YTMPlayer) resumeOrMix(ctx context.Context) (string, error) {
	raw, err := p.col.Status(ctx)
	if err != nil {
		return "", err
	}
	switch nextMusicAction(raw) {
	case "already":
		return summarizeStatus(raw), nil
	case "resume":
		out, err := p.col.Resume(ctx)
		if err != nil {
			return p.playMix(ctx)
		}
		if text, ok := readyLine(out); ok {
			return text, nil
		}
		return p.afterPlay(ctx)
	default:
		return p.playMix(ctx)
	}
}

func (p *YTMPlayer) playMix(ctx context.Context) (string, error) {
	likes, err := p.col.Play(ctx, "мои лайки")
	if err == nil && !libraryUnavailable(likes) {
		return p.afterPlay(ctx)
	}
	recs, recErr := p.col.Recommendations(ctx)
	if recErr == nil && !libraryUnavailable(recs) {
		return p.afterPlay(ctx)
	}
	return "не играет: личные подборки недоступны, пока YouTube Music не подключён, и остановленного трека нет", nil
}

func (p *YTMPlayer) armFromStatus(statusJSON string) {
	track, ok := trackFromStatus(statusJSON)
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.follow.gen++
	p.follow.armed = true
	p.follow.current = track
	p.follow.heard = false
	p.follow.tries = 0
}

func (p *YTMPlayer) disarm() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.follow.gen++
	p.follow.armed = false
	p.follow.heard = false
	p.follow.current = ytmTrack{}
}

func (p *YTMPlayer) followArmed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.follow.armed && !p.follow.busy
}

func (p *YTMPlayer) followGen() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.follow.gen
}

func (p *YTMPlayer) observe(ctx context.Context, statusJSON string) error {
	if p.classify(statusJSON) != watchAdvance {
		return nil
	}
	return p.advance(ctx)
}

const (
	watchWait = iota
	watchAdvance
)

func (p *YTMPlayer) classify(statusJSON string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.follow.armed || p.follow.busy {
		return watchWait
	}
	status := playerStatus(statusJSON)
	track, ok := trackFromStatus(statusJSON)
	matches := ok && sameYTMTitle(track, p.follow.current)
	switch status {
	case "playing":
		if matches {
			p.follow.heard = true
			return watchWait
		}
		if p.follow.heard && ok {
			return watchAdvance
		}
		return watchWait
	case "buffering", "paused":
		if matches {
			p.follow.heard = true
		}
		return watchWait
	case "searching":
		return watchWait
	case "stopped", "idle":
		if p.follow.heard {
			return watchAdvance
		}
		return watchWait
	default:
		return watchWait
	}
}

func (p *YTMPlayer) advance(ctx context.Context) error {
	p.mu.Lock()
	if !p.follow.armed || !p.follow.heard || p.follow.busy {
		p.mu.Unlock()
		return nil
	}
	p.follow.busy = true
	p.follow.armed = false
	p.follow.heard = false
	current := p.follow.current
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.follow.busy = false
		p.mu.Unlock()
	}()

	if p.ask == nil {
		return nil
	}
	raw, err := p.ask(ctx, "music_control", `{"action":"advance"}`)
	if err != nil {
		p.mu.Lock()
		p.follow.tries++
		if p.follow.tries < 3 {
			p.follow.armed = true
			p.follow.heard = true
			p.follow.current = current
		}
		p.mu.Unlock()
		return err
	}
	text, err := p.ApplyIntent(ctx, raw)
	if err != nil || strings.HasPrefix(text, "не ") {
		p.mu.Lock()
		p.follow.tries++
		if p.follow.tries < 3 {
			p.follow.armed = true
			p.follow.heard = true
			p.follow.current = current
		}
		p.mu.Unlock()
	}
	return err
}

func sameYTMTitle(a, b ytmTrack) bool {
	titleA := strings.ToLower(strings.TrimSpace(a.Title))
	titleB := strings.ToLower(strings.TrimSpace(b.Title))
	if titleA != "" && titleA == titleB {
		return true
	}
	return a.ID != "" && a.ID == b.ID
}
