package repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"yascore/internal/domain"
)

// Hub is the in-memory registry of connected skills.
type Hub struct {
	mu     sync.Mutex
	skills map[string]*skillSession
	byTool map[string]string
	player *YTMPlayer
}

// UsePlayer attaches the column music player. Skills only return intents.
func (h *Hub) UsePlayer(p *YTMPlayer) {
	if h == nil {
		return
	}
	h.player = p
	if p != nil {
		p.UseAsk(h.askMusic)
	}
}

// askMusic asks the youtubemusic skill without going through Invoke again.
func (h *Hub) askMusic(ctx context.Context, name, arguments string) (string, error) {
	h.mu.Lock()
	sess := h.skills["youtubemusic"]
	if sess == nil {
		sess = h.skills[h.byTool[name]]
	}
	h.mu.Unlock()
	if sess == nil {
		return "", fmt.Errorf("music skill unavailable")
	}
	return h.ask(ctx, sess, domain.BusMessage{Type: "invoke", Name: name, Arguments: arguments})
}

type skillSession struct {
	name    string
	tools   []domain.Tool
	hints   []string
	sound   bool
	media   bool
	conn    domain.SkillConn
	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan askResult
}

// askResult is one skill reply. err is set when the socket died or the
// result message could not be parsed. A closed channel means disconnect.
type askResult struct {
	body string
	err  error
}

// NewHub returns an empty skill registry. The core runs with zero skills.
func NewHub() *Hub {
	return &Hub{skills: map[string]*skillSession{}, byTool: map[string]string{}}
}

// Count implements domain.SkillBus.
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.skills)
}

// Tools implements domain.SkillBus.
func (h *Hub) Tools(context.Context) []domain.Tool {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []domain.Tool
	for _, s := range h.skills {
		out = append(out, s.tools...)
	}
	return out
}

// Hints implements domain.SkillBus.
func (h *Hub) Hints(context.Context) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, s := range h.skills {
		out = append(out, s.hints...)
	}
	return out
}

// Invoke sends a tool call to the skill that registered it.
// Music tools are executed by the column player; the skill only returns an intent.
func (h *Hub) Invoke(ctx context.Context, call domain.ToolCall) (string, error) {
	if h.player != nil && (call.Name == "play_music" || call.Name == "music_control") {
		return h.invokeMusic(ctx, call)
	}
	h.mu.Lock()
	name := h.byTool[call.Name]
	sess := h.skills[name]
	h.mu.Unlock()
	if sess == nil {
		return "", fmt.Errorf("unknown tool %s", call.Name)
	}
	return h.ask(ctx, sess, domain.BusMessage{Type: "invoke", ID: call.ID, Name: call.Name, Arguments: call.Arguments})
}

func (h *Hub) invokeMusic(ctx context.Context, call domain.ToolCall) (string, error) {
	if h.player == nil {
		h.mu.Lock()
		name := h.byTool[call.Name]
		sess := h.skills[name]
		h.mu.Unlock()
		if sess == nil {
			return "", fmt.Errorf("unknown tool %s", call.Name)
		}
		return h.ask(ctx, sess, domain.BusMessage{Type: "invoke", ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	if call.Name == "music_control" {
		var args struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal([]byte(call.Arguments), &args)
		switch strings.ToLower(strings.TrimSpace(args.Action)) {
		case "pause", "resume", "play", "stop", "status":
			// Bare player commands run on the column. The skill never opens SSH.
			return h.player.Control(ctx, args.Action)
		case "next", "previous", "prev":
			// Queue memory lives in the skill; it returns a play intent or a control.
		case "like":
			return h.player.apply(ctx, domain.MusicIntent{Do: domain.MusicLike})
		case "unlike":
			return h.player.apply(ctx, domain.MusicIntent{Do: domain.MusicUnlike})
		}
	}
	h.mu.Lock()
	name := h.byTool[call.Name]
	sess := h.skills[name]
	h.mu.Unlock()
	if sess == nil {
		return "", fmt.Errorf("unknown tool %s", call.Name)
	}
	raw, err := h.ask(ctx, sess, domain.BusMessage{Type: "invoke", ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	if err != nil {
		return "", err
	}
	return h.player.ApplyIntent(ctx, raw)
}

// PauseAll stops column media through the player when one is attached.
// Otherwise every sound skill still gets a pause message (tests).
// Volume is left alone.
func (h *Hub) PauseAll(ctx context.Context) error {
	var errs []error
	if h.player != nil {
		// Bare «стоп» must stop, not pause: a paused queue would come back
		// after the next wake quiet window.
		if _, err := h.player.Control(ctx, "stop"); err != nil {
			errs = append(errs, err)
		}
		h.mu.Lock()
		var sessions []*skillSession
		for _, sess := range h.skills {
			if sess.sound && !sess.media {
				sessions = append(sessions, sess)
			}
		}
		h.mu.Unlock()
		for _, sess := range sessions {
			raw, err := h.ask(ctx, sess, domain.BusMessage{Type: "pause"})
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if !pauseAccepted(raw) {
				errs = append(errs, errors.New(raw))
			}
		}
		return errors.Join(errs...)
	}
	h.mu.Lock()
	var sessions []*skillSession
	for _, sess := range h.skills {
		if sess.sound {
			sessions = append(sessions, sess)
		}
	}
	h.mu.Unlock()
	for _, sess := range sessions {
		raw, err := h.ask(ctx, sess, domain.BusMessage{Type: "pause"})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !pauseAccepted(raw) {
			errs = append(errs, errors.New(raw))
		}
	}
	return errors.Join(errs...)
}

// PausePlaying pauses the column player and returns the millisecond spot.
// Sound-only skills are left alone. -1 means the player did not report milliseconds.
// Volume is not changed. Without a player (tests), media skills still get pause.
func (h *Hub) PausePlaying(ctx context.Context) (int, error) {
	if h.player != nil {
		return h.player.PausePlaying(ctx)
	}
	return h.pausePlayingSkills(ctx)
}

func (h *Hub) pausePlayingSkills(ctx context.Context) (int, error) {
	h.mu.Lock()
	var sessions []*skillSession
	for _, sess := range h.skills {
		if sess.media {
			sessions = append(sessions, sess)
		}
	}
	h.mu.Unlock()
	pos := -1
	var errs []error
	for _, sess := range sessions {
		raw, err := h.ask(ctx, sess, domain.BusMessage{Type: "pause"})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !pauseAccepted(raw) {
			errs = append(errs, errors.New(raw))
			continue
		}
		if n, ok := positionFromPause(raw); ok && n >= 0 {
			pos = n
		}
	}
	return pos, errors.Join(errs...)
}

// ResumePlaying continues the column player at positionMS.
// A negative position uses the spot the player stored on pause.
func (h *Hub) ResumePlaying(ctx context.Context, positionMS int) error {
	if h.player != nil {
		return h.player.ResumePlaying(ctx, positionMS)
	}
	return h.resumePlayingSkills(ctx, positionMS)
}

func (h *Hub) resumePlayingSkills(ctx context.Context, positionMS int) error {
	h.mu.Lock()
	var sessions []*skillSession
	for _, sess := range h.skills {
		if sess.media {
			sessions = append(sessions, sess)
		}
	}
	h.mu.Unlock()
	if len(sessions) == 0 {
		return fmt.Errorf("no media skill")
	}
	args := ""
	if positionMS >= 0 {
		args = fmt.Sprintf(`{"position_ms":%d}`, positionMS)
	}
	var errs []error
	for _, sess := range sessions {
		raw, err := h.ask(ctx, sess, domain.BusMessage{Type: "resume", Arguments: args})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		raw = strings.TrimSpace(raw)
		switch {
		case raw == "" || raw == "ok" || raw == domain.SilentReply || raw == "включаю" || strings.HasPrefix(raw, "играет ") || pauseAccepted(raw):
		default:
			errs = append(errs, errors.New(raw))
		}
	}
	return errors.Join(errs...)
}

func pauseAccepted(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "ok" || raw == domain.SilentReply {
		return true
	}
	var parsed struct {
		OK bool `json:"ok"`
	}
	return json.Unmarshal([]byte(raw), &parsed) == nil && parsed.OK
}

func positionFromPause(raw string) (int, bool) {
	var parsed struct {
		PositionMS *int `json:"position_ms"`
	}
	if json.Unmarshal([]byte(raw), &parsed) != nil || parsed.PositionMS == nil {
		return 0, false
	}
	return *parsed.PositionMS, true
}

// MediaSkill reports the media skill that owns tool. Ordinary skills are not media.
func (h *Hub) MediaSkill(tool string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	name := h.byTool[tool]
	sess := h.skills[name]
	if sess == nil || !sess.media {
		return "", false
	}
	return name, true
}

// Playing reports whether the column player is making sound.
func (h *Hub) Playing(ctx context.Context) bool {
	if h.player != nil {
		return h.player.Playing(ctx)
	}
	h.mu.Lock()
	var sessions []*skillSession
	for _, sess := range h.skills {
		if sess.media {
			sessions = append(sessions, sess)
		}
	}
	h.mu.Unlock()
	for _, sess := range sessions {
		raw, err := h.ask(ctx, sess, domain.BusMessage{Type: "level"})
		if err != nil {
			continue
		}
		var lv domain.SoundLevel
		if json.Unmarshal([]byte(raw), &lv) != nil {
			continue
		}
		if lv.Active {
			return true
		}
	}
	return false
}

// Resume continues what the column already had, not a new track.
func (h *Hub) Resume(ctx context.Context, name string) (string, error) {
	if h.player != nil {
		if err := h.player.ResumePlaying(ctx, -1); err != nil {
			return "", err
		}
		return domain.SilentReply, nil
	}
	h.mu.Lock()
	sess := h.skills[name]
	h.mu.Unlock()
	if sess == nil || !sess.media {
		return "", fmt.Errorf("no media skill %s", name)
	}
	raw, err := h.ask(ctx, sess, domain.BusMessage{Type: "resume"})
	if err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "" || raw == "ok" || raw == domain.SilentReply || raw == "включаю" || strings.HasPrefix(raw, "играет "):
		return raw, nil
	default:
		return raw, errors.New(raw)
	}
}

// Levels asks every sound skill for its current volume.
func (h *Hub) Levels(ctx context.Context) []domain.SoundLevel {
	h.mu.Lock()
	var sessions []*skillSession
	for _, sess := range h.skills {
		if sess.sound {
			sessions = append(sessions, sess)
		}
	}
	h.mu.Unlock()
	var out []domain.SoundLevel
	for _, sess := range sessions {
		raw, err := h.ask(ctx, sess, domain.BusMessage{Type: "level"})
		if err != nil {
			continue
		}
		var lv domain.SoundLevel
		if json.Unmarshal([]byte(raw), &lv) != nil {
			continue
		}
		lv.Name = sess.name
		out = append(out, lv)
	}
	return out
}

// SetLevel sets one sound skill's volume, 0–100.
func (h *Hub) SetLevel(ctx context.Context, name string, percent int) error {
	h.mu.Lock()
	sess := h.skills[name]
	h.mu.Unlock()
	if sess == nil || !sess.sound {
		return fmt.Errorf("no sound skill %s", name)
	}
	raw, err := h.ask(ctx, sess, domain.BusMessage{Type: "level", Arguments: strconv.Itoa(percent)})
	if err != nil {
		return err
	}
	if strings.TrimSpace(raw) != "ok" {
		return errors.New(raw)
	}
	return nil
}

func (h *Hub) ask(ctx context.Context, sess *skillSession, msg domain.BusMessage) (string, error) {
	ch := make(chan askResult, 1)
	sess.mu.Lock()
	if sess.pending == nil {
		sess.pending = map[string]chan askResult{}
	}
	// Two callers share "player" and "play-button". A taken slot gets a new id
	// so the later result cannot be delivered to the earlier wait.
	if msg.ID == "" || sess.pending[msg.ID] != nil {
		msg.ID = freeID(sess.pending)
	}
	sess.pending[msg.ID] = ch
	sess.mu.Unlock()
	defer func() {
		sess.mu.Lock()
		delete(sess.pending, msg.ID)
		sess.mu.Unlock()
	}()
	raw, _ := json.Marshal(msg)
	sess.writeMu.Lock()
	// The caller's HTTP context ends with that one request. coder/websocket
	// closes the whole skill socket when Write's context is cancelled.
	err := sess.conn.Write(context.WithoutCancel(ctx), raw)
	sess.writeMu.Unlock()
	if err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("timeout: %w", ctx.Err())
	case res, ok := <-ch:
		if !ok {
			return "", errors.New("skill disconnected")
		}
		if res.err != nil {
			return "", res.err
		}
		return res.body, nil
	}
}

// Accept reads register, then results, until the socket closes.
func (h *Hub) Accept(ctx context.Context, conn domain.SkillConn) error {
	raw, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	var reg domain.BusMessage
	if err := json.Unmarshal(raw, &reg); err != nil {
		return err
	}
	if reg.Type != "register" || reg.Name == "" {
		return errors.New("expected register")
	}
	sess := &skillSession{name: reg.Name, tools: reg.Tools, hints: reg.Hints, sound: reg.Sound, media: reg.Media, conn: conn, pending: map[string]chan askResult{}}
	if err := h.add(sess); err != nil {
		_ = writeMsg(ctx, conn, domain.BusMessage{Type: "error", Content: err.Error()})
		return err
	}
	defer h.remove(sess)
	if err := writeMsg(ctx, conn, domain.BusMessage{Type: "registered", Name: reg.Name}); err != nil {
		return err
	}
	for {
		raw, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var msg domain.BusMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			if id := messageID(raw); id != "" {
				deliver(sess, id, askResult{err: fmt.Errorf("invalid result: %w", err)})
			}
			continue
		}
		if msg.Type != "result" {
			continue
		}
		deliver(sess, msg.ID, askResult{body: msg.Content})
	}
}

// deliver sends under the session lock. remove closes those channels under
// the same lock, so a result never lands on a closed channel.
func deliver(sess *skillSession, id string, res askResult) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	ch := sess.pending[id]
	if ch == nil {
		return
	}
	select {
	case ch <- res:
	default:
	}
}

func (h *Hub) add(sess *skillSession) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.skills[sess.name]; exists {
		return fmt.Errorf("skill %s already connected", sess.name)
	}
	for _, tool := range sess.tools {
		if owner, exists := h.byTool[tool.Name]; exists {
			return fmt.Errorf("tool %s already owned by %s", tool.Name, owner)
		}
	}
	h.skills[sess.name] = sess
	for _, tool := range sess.tools {
		h.byTool[tool.Name] = sess.name
	}
	return nil
}

func (h *Hub) remove(sess *skillSession) {
	h.mu.Lock()
	if cur := h.skills[sess.name]; cur == sess {
		delete(h.skills, sess.name)
		for _, tool := range sess.tools {
			if h.byTool[tool.Name] == sess.name {
				delete(h.byTool, tool.Name)
			}
		}
	}
	h.mu.Unlock()
	sess.mu.Lock()
	for id, ch := range sess.pending {
		close(ch)
		delete(sess.pending, id)
	}
	sess.mu.Unlock()
}

func writeMsg(ctx context.Context, conn domain.SkillConn, msg domain.BusMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, raw)
}

func freeID(pending map[string]chan askResult) string {
	for {
		id := newID()
		if _, taken := pending[id]; !taken {
			return id
		}
	}
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var idField = regexp.MustCompile(`"id"\s*:\s*"((?:\\.|[^"\\])*)"`)

// messageID reads an id from a result that did not parse as a whole.
func messageID(raw []byte) string {
	m := idField.FindSubmatch(raw)
	if m == nil {
		return ""
	}
	var id string
	quoted := append(append([]byte{'"'}, m[1]...), '"')
	if json.Unmarshal(quoted, &id) != nil {
		return string(m[1])
	}
	return id
}
