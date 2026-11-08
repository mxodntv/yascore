package usecase

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"yascore/internal/domain"
	"yascore/skills/mood"
)

var (
	maxToolRounds int
	dialogueKeep  int
	dialogueTTL   time.Duration
)

// ConfigureDialogue installs the dialogue window from the loaded config.
func ConfigureDialogue(keep, rounds int, ttl time.Duration) {
	dialogueKeep = keep
	maxToolRounds = rounds
	dialogueTTL = ttl
}

// Utterance runs wake stripping, the model, optional tools, and speech.
type Utterance struct {
	llm     domain.LLM
	wake    domain.Wake
	tts     domain.TTS
	skills  domain.SkillBus
	prompt  string
	moodDir string
	voice   string
	model   string
	maxTok  int
	temp    float64

	memMu    sync.Mutex
	mem      []domain.Message
	memAt    time.Time
	spoken   string
	log      domain.TurnLogger
	commands domain.CommandLogger

	speechMu    sync.Mutex
	speechBusy  int
	speechGen   int
	speechUntil time.Time

	// Media hold: pause on the wake word, resume after the quiet window.
	holdMu    sync.Mutex
	holding   bool
	holdGen   int
	holdPos   int
	holdWait  time.Duration
	holdQuiet time.Duration // test override; zero keeps resumeQuiet
	holdAsk   time.Duration // test override; zero keeps resumeAfterQuestion
}

// pauser is the skill hub. StopAll uses it when the bus can pause sound skills.
type pauser interface {
	PauseAll(ctx context.Context) error
}

// Deps are the ports Answer needs. TTS may be nil.
type Deps struct {
	LLM         domain.LLM
	Wake        domain.Wake
	TTS         domain.TTS
	Skills      domain.SkillBus
	Prompt      string
	MoodDir     string // skill directory; empty uses skills/mood. The choice is data/mood.json.
	Voice       string
	Model       string
	MaxTokens   int
	Temperature float64
	Log         domain.TurnLogger
	Commands    domain.CommandLogger
}

// New builds the utterance use case.
func New(d Deps) *Utterance {
	return &Utterance{
		llm: d.LLM, wake: d.Wake, tts: d.TTS, skills: d.Skills,
		prompt: d.Prompt, moodDir: d.MoodDir, voice: d.Voice, model: d.Model, maxTok: d.MaxTokens, temp: d.Temperature,
		log: d.Log, commands: d.Commands,
	}
}

// HandleUtterance turns one line into a reply, keeping the recent dialogue on this process.
func (u *Utterance) HandleUtterance(ctx context.Context, text string) (domain.Reply, error) {
	ctx, scope := u.openCommand(ctx, "http")
	defer scope.Finish()
	ctx, owner := beginTrace(ctx)
	defer u.flushTrace(owner)
	return u.reply(ctx, nil, text)
}

// HandleChat is the column path. prior is the user and assistant turns before the latest line.
// An empty prior uses the dialogue this process already remembers.
func (u *Utterance) HandleChat(ctx context.Context, prior []domain.Message, text string) (domain.Reply, error) {
	ctx, scope := u.openCommand(ctx, "http")
	defer scope.Finish()
	ctx, owner := beginTrace(ctx)
	defer u.flushTrace(owner)
	return u.reply(ctx, prior, text)
}

func (u *Utterance) reply(ctx context.Context, prior []domain.Message, text string) (domain.Reply, error) {
	raw := text
	text = u.strip(text)
	rememberHeard(ctx, raw, text)
	heard := text
	// The last sentence is echo only while it is still on the speaker.
	// After speechUntil, the same words are the next turn.
	spoken := u.echoSpoken()
	text = domain.EchoStrip(text, spoken)
	// A short follow-up reuses words she just said. That is the next turn,
	// not the speaker. Only a real playback echo is dropped.
	if strings.TrimSpace(text) == "" && strings.TrimSpace(heard) != "" && !domain.PureEcho(heard, spoken) {
		text = strings.TrimSpace(heard)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		if heard != "" {
			u.record(ctx, domain.Conversation{User: heard, Reply: domain.EchoReply, Stack: "echo-drop"})
		} else {
			u.record(ctx, domain.Conversation{User: heard, Error: domain.ErrEmpty.Error(), Stack: stackFrom(domain.ErrEmpty)})
		}
		return domain.Reply{}, domain.ErrEmpty
	}
	// Wake word accepted: pause media before stop, tools, and speech
	// so the track does not play under the reply. The quiet window resumes it.
	if u.woke(raw) {
		u.pauseOnWake(ctx)
	} else if u.mediaHolding() {
		u.cancelMediaResume()
	}
	if globalStop(text) {
		return u.stopAll(ctx, text)
	}
	// While this process is still speaking, the microphone hears her voice.
	// A new command waits. Stop above already aborted that speech.
	if u.suppressEcho() {
		u.record(ctx, domain.Conversation{User: heard, Reply: domain.EchoReply, Stack: "echo-drop"})
		return domain.Reply{}, domain.ErrEmpty
	}
	past := u.turns(prior)
	if len(past) == 0 {
		past = u.recall()
	}
	if cmd, ok := playerAction(text); ok && u.skills != nil {
		return u.player(ctx, past, text, cmd)
	}
	messages := append(append([]domain.Message{}, past...), domain.Message{Role: "user", Content: text})
	choice := "auto"
	if wantsTools(text, hintsOf(ctx, u.skills)) && len(toolsOf(ctx, u.skills)) > 0 {
		choice = "required"
	}
	var last domain.Completion
	var traced []domain.ToolTrace
	for round := 0; round < maxToolRounds; round++ {
		tools := toolsOf(ctx, u.skills)
		req := domain.CompleteRequest{
			System: u.systemPrompt(), Messages: messages, Tools: tools, ToolChoice: choice,
			Model: u.model, MaxTokens: u.maxTok, Temperature: u.temp,
		}
		if len(tools) == 0 {
			req.ToolChoice = ""
			req.Tools = nil
		}
		var comp domain.Completion
		err := u.timed(ctx, "llm", "reply", "llm", func() error {
			var callErr error
			comp, callErr = u.llm.Complete(ctx, req)
			return callErr
		})
		if err != nil {
			u.record(ctx, domain.Conversation{User: text, Error: err.Error(), Stack: stackFrom(err), Tools: traced})
			return domain.Reply{}, err
		}
		last = comp
		askedTool := choice == "required"
		choice = "auto"
		if len(comp.ToolCalls) == 0 && askedTool && round == 0 && u.skills != nil {
			messages = append(messages, domain.Message{
				Role:    "user",
				Content: "Вызови инструмент и дождись его ответа. Не описывай результат заранее.",
			})
			choice = "required"
			continue
		}
		if len(comp.ToolCalls) == 0 || u.skills == nil {
			out := strings.TrimSpace(comp.Text)
			if out == "" {
				out = "Готово."
			}
			return u.finish(ctx, past, text, domain.Reply{Text: out, Model: comp.Model}, traced, branchOf(traced)), nil
		}
		messages = append(messages, domain.Message{
			Role: "assistant", Content: comp.Text, ToolCalls: comp.ToolCalls,
		})
		var spoken string
		for _, call := range comp.ToolCalls {
			var result string
			err := u.timed(ctx, toolCallName(call.Name), "reply", toolDetail(call), func() error {
				var callErr error
				result, callErr = u.skills.Invoke(ctx, call)
				return callErr
			})
			if err != nil {
				result = err.Error()
			}
			traced = append(traced, domain.ToolTrace{Name: call.Name, Arguments: call.Arguments, Result: result})
			spoken = result
			messages = append(messages, domain.Message{
				Role: "tool", ToolCallID: call.ID, Content: result,
			})
		}
		if len(comp.ToolCalls) == 1 && playerSentence(spoken) {
			line := strings.TrimSpace(spoken)
			reply := domain.Reply{Text: line, Model: comp.Model, Quiet: musicQuiet(line)}
			return u.finish(ctx, past, text, reply, traced, branchOf(traced)), nil
		}
		if len(comp.ToolCalls) == 1 && comp.ToolCalls[0].Name == "set_mood" {
			line := strings.TrimSpace(spoken)
			if line == "" {
				line = "Готово."
			}
			return u.finish(ctx, past, text, domain.Reply{Text: line, Model: comp.Model}, traced, branchOf(traced)), nil
		}
		if len(comp.ToolCalls) == 1 {
			if line, ok := readySpeech(spoken); ok {
				return u.finish(ctx, past, text, domain.Reply{Text: line, Model: comp.Model}, traced, branchOf(traced)), nil
			}
		}
	}
	if strings.TrimSpace(last.Text) != "" {
		return u.finish(ctx, past, text, domain.Reply{Text: strings.TrimSpace(last.Text), Model: last.Model}, traced, branchOf(traced)), nil
	}
	return u.finish(ctx, past, text, domain.Reply{Text: "Не получилось закончить команду.", Model: last.Model}, traced, branchOf(traced)), nil
}

// systemPrompt is the base prompt plus the saved mood, read on every model call.
// No saved mood leaves the base text as it is.
func (u *Utterance) systemPrompt() string {
	return mood.BuildPrompt(u.prompt, u.moodDir)
}

func (u *Utterance) finish(ctx context.Context, past []domain.Message, user string, reply domain.Reply, tools []domain.ToolTrace, branch string) domain.Reply {
	turns := append(append([]domain.Message{}, past...), domain.Message{Role: "user", Content: user}, domain.Message{Role: "assistant", Content: reply.Text})
	if len(turns) > dialogueKeep {
		turns = turns[len(turns)-dialogueKeep:]
	}
	if len(turns) > 0 && turns[0].Role != "user" {
		turns = turns[1:]
	}
	u.memMu.Lock()
	u.mem = turns
	u.memAt = time.Now()
	if !reply.Quiet {
		if spoken := strings.TrimSpace(reply.Text); spoken != "" && spoken != domain.SilentReply && spoken != domain.EchoReply {
			u.spoken = spoken
		}
	}
	u.memMu.Unlock()
	u.record(ctx, domain.Conversation{
		User: user, Reply: reply.Text, Quiet: reply.Quiet, Model: reply.Model, Stack: branch, Tools: tools,
	})
	if u.mediaHolding() {
		u.scheduleMediaResume(u.quietFor(reply))
	}
	return reply
}

// StopAll pauses every connected sound skill, forgets the dialogue, and stays quiet.
func (u *Utterance) StopAll(ctx context.Context) (domain.Reply, error) {
	return u.stopAll(ctx, "")
}

func (u *Utterance) stopAll(ctx context.Context, user string) (domain.Reply, error) {
	ctx, scope := u.openCommand(ctx, "http")
	defer scope.Finish()
	ctx, owner := beginTrace(ctx)
	defer u.flushTrace(owner)
	u.clearMediaHold()
	start := time.Now()
	var err error
	if p, ok := u.skills.(pauser); ok && p != nil {
		err = u.timed(ctx, "pause", "stopAll", "stop", func() error {
			return p.PauseAll(ctx)
		})
	}
	NoteAction(ctx, "stop-all", "stopAll", "stop", start, time.Now(), err)
	u.memMu.Lock()
	u.mem = nil
	u.memMu.Unlock()
	u.clearSpeech()
	reply := domain.Reply{Text: domain.StoppedReply, Quiet: true}
	rec := domain.Conversation{User: user, Reply: reply.Text, Quiet: true, Stack: "stop-all"}
	if err != nil {
		rec.Error = err.Error()
		rec.Stack = stackFrom(err)
	}
	u.record(ctx, rec)
	return reply, err
}

func (u *Utterance) lastSpoken() string {
	u.memMu.Lock()
	defer u.memMu.Unlock()
	return u.spoken
}

// echoSpoken is the line to strip, and only while audio is still playing.
func (u *Utterance) echoSpoken() string {
	if !u.speaking() {
		return ""
	}
	return u.lastSpoken()
}

func (u *Utterance) recall() []domain.Message {
	u.memMu.Lock()
	defer u.memMu.Unlock()
	if len(u.mem) == 0 || time.Since(u.memAt) > dialogueTTL {
		return nil
	}
	return append([]domain.Message{}, u.mem...)
}

func (u *Utterance) turns(in []domain.Message) []domain.Message {
	var out []domain.Message
	for _, m := range in {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		content := strings.TrimSpace(m.Content)
		if content == "" || (role != "user" && role != "assistant") {
			continue
		}
		if role == "assistant" && (content == domain.SilentReply || content == domain.EchoReply) {
			continue
		}
		if role == "user" {
			content = u.strip(content)
			if content == "" {
				continue
			}
		}
		out = append(out, domain.Message{Role: role, Content: content})
	}
	if len(out) > dialogueKeep {
		out = out[len(out)-dialogueKeep:]
	}
	if len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out
}

func (u *Utterance) strip(text string) string {
	if u.wake != nil {
		text = u.wake.Strip(text)
	}
	return strings.TrimSpace(text)
}

// Speak synthesizes text with the configured voice.
// A speak that is not already part of an utterance is its own journal line.
func (u *Utterance) Speak(ctx context.Context, text string) (domain.Audio, error) {
	if u.tts == nil {
		return domain.Audio{}, fmt.Errorf("tts is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.Audio{}, domain.ErrEmpty
	}
	ctx, scope := u.openCommand(ctx, "http")
	defer scope.Finish()
	ctx, owner := beginTrace(ctx)
	defer u.flushTrace(owner)
	gen := u.beginSpeech()
	defer u.endSpeech()
	var audio domain.Audio
	err := u.timed(ctx, "tts", "Speak", "tts", func() error {
		var callErr error
		audio, callErr = u.tts.Synthesize(ctx, text, u.voice)
		return callErr
	})
	if err != nil {
		noteFailure(ctx, err)
	}
	if owner != nil {
		rec := domain.Conversation{Reply: text, Stack: "speak"}
		if err != nil {
			rec.Error = err.Error()
		}
		u.record(ctx, rec)
	}
	if err != nil {
		return domain.Audio{}, err
	}
	if u.speechStale(gen) {
		return domain.Audio{}, nil
	}
	return audio, nil
}

// Answer is HandleUtterance followed by Speak when a voice is configured.
func (u *Utterance) Answer(ctx context.Context, text string) (domain.Reply, domain.Audio, error) {
	ctx, scope := u.openCommand(ctx, "http")
	defer scope.Finish()
	ctx, owner := beginTrace(ctx)
	defer u.flushTrace(owner)
	reply, err := u.HandleUtterance(ctx, text)
	if err != nil {
		return reply, domain.Audio{}, err
	}
	if u.tts == nil || reply.Quiet {
		return reply, domain.Audio{}, nil
	}
	audio, err := u.Speak(ctx, reply.Text)
	return reply, audio, err
}

// Addressed reports whether text starts with the configured name.
func (u *Utterance) Addressed(text string) bool {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return false
	}
	if u.wake == nil {
		return true
	}
	if strings.TrimSpace(u.wake.Strip(raw)) != raw {
		return true
	}
	return strings.TrimSpace(u.wake.Strip(raw+" да")) == "да"
}

// readySpeech is a tool result the user can hear as it is.
// A weather forecast is that sentence; asks_today is only a fact for a model
// that will not be called again. A short fragment still goes back to the model.
func readySpeech(result string) (string, bool) {
	result = strings.TrimSpace(result)
	if result == "" {
		return "", false
	}
	lines := strings.Split(result, "\n")
	sentence := strings.TrimSpace(lines[0])
	if sentence == "" {
		return "", false
	}
	for _, extra := range lines[1:] {
		extra = strings.TrimSpace(extra)
		if extra == "" {
			continue
		}
		key, val, ok := strings.Cut(extra, ":")
		if !ok || strings.TrimSpace(key) != "asks_today" || strings.TrimSpace(val) == "" {
			return "", false
		}
	}
	if !spokenLine(sentence) {
		return "", false
	}
	return sentence, true
}

// spokenLine is a finished phrase. "В городе … сейчас …" is the weather sentence.
// Anything else has to end the way a spoken line ends. "в Москве ясно" does not.
func spokenLine(s string) bool {
	if strings.HasPrefix(s, "В городе ") && strings.Contains(s, " сейчас ") {
		return true
	}
	r := []rune(s)
	if len(r) == 0 || !strings.Contains(s, " ") {
		return false
	}
	switch r[len(r)-1] {
	case '.', '!', '?', '…':
		return true
	default:
		return false
	}
}

func playerSentence(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "играет ") || strings.HasPrefix(text, "не играет") || text == "включаю" || strings.HasPrefix(text, "плеер:") || text == domain.SilentReply || spokenLibrary(text)
}

// spokenLibrary is a short sentence the user has to hear. It is not a quiet success.
func spokenLibrary(text string) bool {
	switch strings.TrimSpace(text) {
	case domain.NoLikesYet, domain.NothingToLike, domain.NotInLikes, domain.LikedReply, domain.UnlikedReply:
		return true
	default:
		return false
	}
}

// musicQuiet is a successful player result: the column should not read the track aloud.
func musicQuiet(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "играет ") || text == "включаю" || text == domain.SilentReply
}

func (u *Utterance) player(ctx context.Context, past []domain.Message, text string, cmd playerCommand) (domain.Reply, error) {
	if cmd.keepHold {
		// Like/unlike speak a short confirmation; keep (or arm) the wake hold so
		// music resumes after TTS instead of staying paused.
		if !u.mediaHolding() {
			if _, ok := u.skills.(mediaPauser); ok {
				u.pauseOnWake(ctx)
			}
		}
	} else {
		// Next/stop/play own the column — do not resume a prior wake hold over them.
		u.clearMediaHold()
	}
	call := domain.ToolCall{ID: "player", Name: cmd.tool, Arguments: cmd.args}
	var result string
	err := u.timed(ctx, toolCallName(call.Name), "player", toolDetail(call), func() error {
		var callErr error
		result, callErr = u.skills.Invoke(ctx, call)
		return callErr
	})
	trace := []domain.ToolTrace{{Name: cmd.tool, Arguments: cmd.args}}
	if err != nil {
		trace[0].Result = err.Error()
		return u.finish(ctx, past, text, domain.Reply{Text: err.Error(), Instant: true}, trace, "music-control"), nil
	}
	result = strings.TrimSpace(result)
	trace[0].Result = result
	if spokenLibrary(result) || strings.HasPrefix(result, "не ") || strings.Contains(strings.ToLower(result), "ошиб") {
		return u.finish(ctx, past, text, domain.Reply{Text: result, Instant: true}, trace, "music-control"), nil
	}
	if result == "" || result == "ok" || result == domain.SilentReply || musicQuiet(result) {
		return u.finish(ctx, past, text, domain.Reply{Text: cmd.memory, Quiet: true, Instant: true}, trace, "music-control"), nil
	}
	return u.finish(ctx, past, text, domain.Reply{Text: cmd.memory, Quiet: true, Instant: true}, trace, "music-control"), nil
}

func toolsOf(ctx context.Context, bus domain.SkillBus) []domain.Tool {
	if bus == nil {
		return nil
	}
	return bus.Tools(ctx)
}

func hintsOf(ctx context.Context, bus domain.SkillBus) []string {
	if bus == nil {
		return nil
	}
	return bus.Hints(ctx)
}
