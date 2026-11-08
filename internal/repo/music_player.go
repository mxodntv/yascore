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

// MusicMemory is the shared core music store for pause, likes, and history.
type MusicMemory interface {
	SavePlayback(media string, title, artist, externalID string, positionMS int) error
	LoadPlayback(media string) (title, artist, externalID string, positionMS int, ok bool, err error)
	ClearPlayback(media string) error
	RandomLike() (title, artist, externalID string, ok bool, err error)
	// RecordStart writes tracks + history; appendQueue true promotes an existing
	// queued play (or appends if missing) — used for next/prev/auto-switch.
	RecordStart(title, artist, externalID, skill string, at time.Time, appendQueue bool) error
	FinishListen(listenedMS int) error
	EnsureQueued(titles, artists, externalIDs []string, skill string) error
}

const (
	prefetchBack  = 2
	prefetchAhead = 3
)

// MusicPlayer is the core's column music player. Skills only return intents.
type MusicPlayer struct {
	col     *Column
	ask     IntentAsk
	service string
	memory  MusicMemory

	mu            sync.Mutex
	lastPos       int
	follow        musicFollow
	prefetch      map[string]struct{} // ids already cache/add'd this process
	prefetchGen   uint64              // bumps on each schedule; stale goroutines exit
	prefetchDelay time.Duration       // zero → 5s; tests set short
	prefetchGap   time.Duration       // zero → 400ms between SSH
}

type musicFollow struct {
	armed   bool
	heard   bool
	busy    bool
	tries   int
	gen     uint64
	current musicTrack
}

// NewMusicPlayer builds the player. service is the music site base URL.
// ask may be nil until the hub wires it.
func NewMusicPlayer(col *Column, service string) *MusicPlayer {
	return &MusicPlayer{col: col, service: strings.TrimRight(strings.TrimSpace(service), "/"), lastPos: -1}
}

// UseAsk sets how the player asks the music skill for intents (advance, like).
func (p *MusicPlayer) UseAsk(ask IntentAsk) {
	if p == nil {
		return
	}
	p.ask = ask
}

// UseMemory attaches the shared music database for pause and history.
func (p *MusicPlayer) UseMemory(memory MusicMemory) {
	if p == nil {
		return
	}
	p.memory = memory
}

// StartWatch polls the column while a queued track is armed.
func (p *MusicPlayer) StartWatch(ctx context.Context) {
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
func (p *MusicPlayer) PausePlaying(ctx context.Context) (int, error) {
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
func (p *MusicPlayer) ResumePlaying(ctx context.Context, positionMS int) error {
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

func (p *MusicPlayer) seekAfterResume(ctx context.Context, currentMS, positionMS int) error {
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
func (p *MusicPlayer) Playing(ctx context.Context) bool {
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
func (p *MusicPlayer) Control(ctx context.Context, action string) (string, error) {
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
		p.finishListen(ctx)
		p.disarm()
		if _, err := p.col.Stop(ctx); err != nil {
			return "", err
		}
		return domain.SilentReply, nil
	case "pause":
		pos, err := p.PausePlaying(ctx)
		if err != nil {
			return "", err
		}
		p.finishListenMS(pos)
		p.saveUserPause(ctx, pos)
		return domain.SilentReply, nil
	case "resume", "play":
		if err := p.resumeFromMemoryOrLast(ctx); err != nil {
			return "", err
		}
		return domain.SilentReply, nil
	case "next":
		if _, err := p.col.Next(ctx); err != nil {
			return "", err
		}
		go p.afterSkip(true)
		return domain.SilentReply, nil
	case "previous", "prev":
		if _, err := p.col.Previous(ctx); err != nil {
			return "", err
		}
		go p.afterSkip(true)
		return domain.SilentReply, nil
	default:
		return "", fmt.Errorf("неизвестная команда плеера: %s", action)
	}
}

// ApplyIntent runs one skill intent on the column.
func (p *MusicPlayer) ApplyIntent(ctx context.Context, raw string) (string, error) {
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

func (p *MusicPlayer) apply(ctx context.Context, intent domain.MusicIntent) (string, error) {
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
	case domain.MusicPlayMix:
		return p.playMix(ctx)
	case domain.MusicResumeOnly:
		return p.resumeOnly(ctx)
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

func rawFromAsk(ctx context.Context, p *MusicPlayer, name, args string) string {
	if p == nil || p.ask == nil {
		return ""
	}
	raw, err := p.ask(ctx, name, args)
	if err != nil {
		return ""
	}
	return raw
}

func (p *MusicPlayer) likeOrUnlike(ctx context.Context, do string) (string, error) {
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

func (p *MusicPlayer) searchPhrase(ctx context.Context, query string) (string, error) {
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

func (p *MusicPlayer) startPhrase(ctx context.Context, phrase string, follow bool) (string, error) {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return "", fmt.Errorf("не поняла, что включить")
	}
	p.finishListen(ctx)
	var last string
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := p.col.Play(ctx, phrase)
		if err != nil {
			return "", err
		}
		if text, ok := readyLine(raw); ok {
			if status, stErr := p.col.Status(ctx); stErr == nil {
				if follow {
					p.armFromStatus(status)
				}
				p.recordCurrent(status, false)
			} else if follow {
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
			if status, stErr := p.col.Status(ctx); stErr == nil {
				if follow {
					p.armFromStatus(status)
				}
				p.recordCurrent(status, false)
			}
			return text, nil
		}
	}
	return last, nil
}

func (p *MusicPlayer) afterPlay(ctx context.Context) (string, error) {
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

func (p *MusicPlayer) resumeOrMix(ctx context.Context) (string, error) {
	raw, err := p.col.Status(ctx)
	if err == nil && nextMusicAction(raw) == "already" {
		return summarizeStatus(raw), nil
	}
	if text, ok, err := p.resumeStored(ctx); err != nil {
		return "", err
	} else if ok {
		return text, nil
	}
	if title, artist, _, ok, err := p.randomLike(); err != nil {
		return "", err
	} else if ok {
		query := strings.TrimSpace(strings.TrimSpace(title) + " " + strings.TrimSpace(artist))
		if query == "" {
			return p.playMix(ctx)
		}
		return p.ApplyIntent(ctx, mustJSON(domain.MusicIntent{Do: domain.MusicSearchPlay, Query: query, Follow: true}))
	}
	return p.playMix(ctx)
}

func (p *MusicPlayer) resumeOnly(ctx context.Context) (string, error) {
	text, ok, err := p.resumeStored(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		return "нечего продолжать", nil
	}
	return text, nil
}

func (p *MusicPlayer) resumeStored(ctx context.Context) (string, bool, error) {
	title, artist, videoID, ms, ok, err := p.loadPause()
	if err != nil || !ok {
		return "", false, err
	}
	if err := p.resumeFromPause(ctx, title, artist, videoID, ms); err != nil {
		return "", false, err
	}
	text, err := p.afterPlay(ctx)
	if err != nil {
		return "", true, err
	}
	return text, true, nil
}

func (p *MusicPlayer) resumeFromMemoryOrLast(ctx context.Context) error {
	_, _, _, ms, ok, err := p.loadPause()
	if err != nil {
		return err
	}
	if ok {
		return p.ResumePlaying(ctx, ms)
	}
	return p.ResumePlaying(ctx, -1)
}

func (p *MusicPlayer) resumeFromPause(ctx context.Context, title, artist, externalID string, ms int) error {
	raw, err := p.col.Status(ctx)
	if err == nil {
		switch nextMusicAction(raw) {
		case "resume", "already":
			if err := p.ResumePlaying(ctx, ms); err == nil {
				_ = p.clearPause()
				return nil
			}
		}
	}
	phrase := strings.TrimSpace(strings.TrimSpace(title) + " " + strings.TrimSpace(artist))
	if phrase == "" && externalID != "" {
		phrase = p.service + "/watch?v=" + externalID
	}
	if phrase == "" {
		return fmt.Errorf("нечего продолжать")
	}
	if _, err := p.startPhrase(ctx, phrase, true); err != nil {
		return err
	}
	if ms > 0 {
		if err := p.seekAfterResume(ctx, 0, ms); err != nil {
			return err
		}
	}
	_ = p.clearPause()
	return nil
}

func (p *MusicPlayer) saveUserPause(ctx context.Context, pos int) {
	if p == nil || p.memory == nil || pos < 0 {
		return
	}
	raw, err := p.col.Status(ctx)
	title, artist, videoID := "", "", ""
	if err == nil {
		if track, ok := trackFromStatus(raw); ok {
			title, artist, videoID = track.Title, track.Artists, track.ID
		}
	}
	_ = p.memory.SavePlayback("music", title, artist, videoID, pos)
}

func (p *MusicPlayer) loadPause() (title, artist, videoID string, ms int, ok bool, err error) {
	if p == nil || p.memory == nil {
		return "", "", "", -1, false, nil
	}
	return p.memory.LoadPlayback("music")
}

func (p *MusicPlayer) clearPause() error {
	if p == nil || p.memory == nil {
		return nil
	}
	return p.memory.ClearPlayback("music")
}

func (p *MusicPlayer) randomLike() (title, artist, videoID string, ok bool, err error) {
	if p == nil || p.memory == nil {
		return "", "", "", false, nil
	}
	return p.memory.RandomLike()
}

func mustJSON(intent domain.MusicIntent) string {
	body, _ := json.Marshal(intent)
	return string(body)
}

func (p *MusicPlayer) playMix(ctx context.Context) (string, error) {
	p.finishListen(ctx)
	likes, err := p.col.Play(ctx, "мои лайки")
	if err == nil && !libraryUnavailable(likes) {
		text, afterErr := p.afterPlay(ctx)
		p.recordAfterColumn(ctx, true)
		return text, afterErr
	}
	recs, recErr := p.col.Recommendations(ctx)
	if recErr == nil && !libraryUnavailable(recs) {
		text, afterErr := p.afterPlay(ctx)
		p.recordAfterColumn(ctx, true)
		return text, afterErr
	}
	return "не играет: личные подборки недоступны, пока YouTube Music не подключён, и остановленного трека нет", nil
}

func (p *MusicPlayer) armFromStatus(statusJSON string) {
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

func (p *MusicPlayer) afterSkip(appendQueue bool) {
	p.finishListenMS(0)
	time.Sleep(300 * time.Millisecond)
	p.recordAfterColumn(context.Background(), appendQueue)
}

func (p *MusicPlayer) recordAfterColumn(ctx context.Context, appendQueue bool) {
	if p == nil || p.col == nil {
		return
	}
	status, err := p.col.Status(ctx)
	if err != nil {
		return
	}
	p.recordCurrent(status, appendQueue)
}

func (p *MusicPlayer) recordCurrent(statusJSON string, appendQueue bool) {
	if p == nil {
		return
	}
	track, ok := trackFromStatus(statusJSON)
	if !ok {
		return
	}
	if p.memory != nil {
		_ = p.memory.RecordStart(track.Title, track.Artists, track.ID, "musicyoutube", time.Now().UTC(), appendQueue)
	}
	p.queueAhead(statusJSON)
	p.schedulePrefetch(statusJSON)
}

func (p *MusicPlayer) queueAhead(statusJSON string) {
	if p == nil || p.memory == nil {
		return
	}
	queue, index, ok := queueFromStatus(statusJSON)
	if !ok {
		return
	}
	_, forward := neighborTracks(queue, index, 0, prefetchAhead)
	if len(forward) == 0 {
		return
	}
	titles := make([]string, len(forward))
	artists := make([]string, len(forward))
	ids := make([]string, len(forward))
	for i, t := range forward {
		titles[i], artists[i], ids[i] = t.Title, t.Artists, t.ID
	}
	_ = p.memory.EnsureQueued(titles, artists, ids, "musicyoutube")
}

func (p *MusicPlayer) schedulePrefetch(statusJSON string) {
	if p == nil || p.col == nil {
		return
	}
	queue, index, ok := queueFromStatus(statusJSON)
	if !ok {
		return
	}
	behind, forward := neighborIDs(queue, index, prefetchBack, prefetchAhead)
	if len(behind) == 0 && len(forward) == 0 {
		return
	}
	p.mu.Lock()
	p.prefetchGen++
	gen := p.prefetchGen
	delay := p.prefetchDelay
	gap := p.prefetchGap
	p.mu.Unlock()
	if delay <= 0 {
		delay = 5 * time.Second
	}
	if gap <= 0 {
		gap = 400 * time.Millisecond
	}
	go func() {
		time.Sleep(delay)
		if !p.prefetchCurrent(gen) {
			return
		}
		ctx := context.Background()
		// Neighbors are already in the column queue — only warm ytm cache.
		for _, id := range forward {
			if !p.prefetchCurrent(gen) {
				return
			}
			p.cachePrefetch(ctx, id, gap)
		}
		for _, id := range behind {
			if !p.prefetchCurrent(gen) {
				return
			}
			p.cachePrefetch(ctx, id, gap)
		}
	}()
}

func (p *MusicPlayer) prefetchCurrent(gen uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return gen == p.prefetchGen
}

func (p *MusicPlayer) cachePrefetch(ctx context.Context, id string, gap time.Duration) {
	key := "c:" + id
	if !p.markPrefetch(key) {
		return
	}
	if _, err := p.col.CacheAdd(ctx, id); err != nil {
		p.unmarkPrefetch(key)
		return
	}
	if gap > 0 {
		time.Sleep(gap)
	}
}

func (p *MusicPlayer) markPrefetch(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prefetch == nil {
		p.prefetch = map[string]struct{}{}
	}
	if _, seen := p.prefetch[key]; seen {
		return false
	}
	p.prefetch[key] = struct{}{}
	return true
}

func (p *MusicPlayer) unmarkPrefetch(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.prefetch, key)
}

func (p *MusicPlayer) finishListen(ctx context.Context) {
	if p == nil || p.memory == nil || p.col == nil {
		return
	}
	raw, err := p.col.Status(ctx)
	if err != nil {
		_ = p.memory.FinishListen(0)
		return
	}
	p.finishListenMS(spotFromStatus(raw).MS)
}

func (p *MusicPlayer) finishListenMS(ms int) {
	if p == nil || p.memory == nil {
		return
	}
	if ms < 0 {
		ms = 0
	}
	_ = p.memory.FinishListen(ms)
}

func (p *MusicPlayer) disarm() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.follow.gen++
	p.follow.armed = false
	p.follow.heard = false
	p.follow.current = musicTrack{}
}

func (p *MusicPlayer) followArmed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.follow.armed && !p.follow.busy
}

func (p *MusicPlayer) followGen() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.follow.gen
}

func (p *MusicPlayer) observe(ctx context.Context, statusJSON string) error {
	switch p.classify(statusJSON) {
	case watchSwitched:
		p.finishListen(ctx)
		p.recordCurrent(statusJSON, true) // also prefetches ± neighbors
		p.armFromStatus(statusJSON)
		return nil
	case watchAdvance:
		return p.advance(ctx)
	default:
		return nil
	}
}

const (
	watchWait = iota
	watchAdvance
	watchSwitched // column already started the next track without a voice command
)

func (p *MusicPlayer) classify(statusJSON string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.follow.armed || p.follow.busy {
		return watchWait
	}
	status := playerStatus(statusJSON)
	track, ok := trackFromStatus(statusJSON)
	matches := ok && sameMusicTitle(track, p.follow.current)
	switch status {
	case "playing":
		if matches {
			p.follow.heard = true
			return watchWait
		}
		if p.follow.heard && ok {
			return watchSwitched
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

func (p *MusicPlayer) advance(ctx context.Context) error {
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

func sameMusicTitle(a, b musicTrack) bool {
	titleA := strings.ToLower(strings.TrimSpace(a.Title))
	titleB := strings.ToLower(strings.TrimSpace(b.Title))
	if titleA != "" && titleA == titleB {
		return true
	}
	return a.ID != "" && a.ID == b.ID
}
