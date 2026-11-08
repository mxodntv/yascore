package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"yascore/internal/domain"
	"yascore/internal/usecase"
)

// Info is what /healthz reports. Skills may be nil.
type Info struct {
	Assistant   string
	LLM         string
	STT         string
	TTS         string
	Skills      func() int
	VoiceVolume int
}

// Server is the HTTP/WS edge. It calls use cases and does not talk to models itself.
type Server struct {
	utt        *usecase.Utterance
	skills     domain.SkillAcceptor
	info       Info
	audioDir   string
	publicBase string
	duck       *usecase.Ducker
	voice      int
	commands   domain.CommandLogger
}

// New builds the HTTP handler.
func New(utt *usecase.Utterance, skills domain.SkillAcceptor, info Info, audioDir, publicBase string) *Server {
	voice := info.VoiceVolume
	if voice <= 0 || voice > 100 {
		voice = 100
	}
	return &Server{utt: utt, skills: skills, info: info, audioDir: audioDir, publicBase: strings.TrimRight(publicBase, "/"), voice: voice}
}

// UseDucking lowers playing skills while a spoken reply is on the air.
func (s *Server) UseDucking(d *usecase.Ducker) {
	s.duck = d
}

// UseCommands records every HTTP command in the shared journal.
func (s *Server) UseCommands(log domain.CommandLogger) {
	s.commands = log
}

// Handler routes health, utterance, audio files, and the skill socket.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /v1/utterance", s.utterance)
	mux.HandleFunc("POST /v1/chat/completions", s.chatCompletions)
	mux.HandleFunc("POST /v1/speak", s.speak)
	mux.HandleFunc("GET /audio/{name}", s.audio)
	if s.skills != nil {
		mux.HandleFunc("GET /v1/skills", s.skillsWS)
	}
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	n := 0
	if s.info.Skills != nil {
		n = s.info.Skills()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"assistant": s.info.Assistant,
		"llm":       s.info.LLM,
		"stt":       s.info.STT,
		"tts":       s.info.TTS,
		"skills":    n,
	})
}

func (s *Server) utterance(w http.ResponseWriter, r *http.Request) {
	ctx, scope := usecase.OpenCommand(context.WithoutCancel(r.Context()), "http", s.commands)
	var body struct {
		Text string `json:"text"`
	}
	if decErr := json.NewDecoder(r.Body).Decode(&body); decErr != nil {
		scope.SetOutcome("text required")
		scope.Finish()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text required"})
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		scope.SetOutcome("text required")
		scope.Finish()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text required"})
		return
	}
	// The core pauses media skills on the wake word inside Answer. This request
	// does not touch the player volume: 1 is full scale on that player, not one percent.
	reply, audio, err := s.utt.Answer(ctx, body.Text)
	if err != nil && reply.Text == "" {
		scope.Finish()
		code := http.StatusBadGateway
		if errors.Is(err, domain.ErrEmpty) {
			code = http.StatusBadRequest
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	out := map[string]any{"text": reply.Text, "model": reply.Model}
	if len(audio.Bytes) > 0 {
		hold := domain.WAVDuration(audio.Bytes)
		if !reply.Instant {
			hold += 4 * time.Second
		}
		s.utt.HoldSpeech(hold)
		url, saveErr := s.saveAudio(audio)
		if saveErr != nil {
			scope.Finish()
			out["tts_error"] = saveErr.Error()
		} else {
			scope.Finish()
			out["audio_url"] = url
			out["voice"] = audio.Voice
		}
	} else {
		scope.Finish()
	}
	if err != nil {
		out["tts_error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// startDuck lowers playing music for the column speak path.
// It writes the level once. The old 300ms reapply turned a ducked "1"
// into full volume, because this player treats 1 as the top of 0..1.
func (s *Server) startDuck(ctx context.Context) int {
	if s == nil || s.duck == nil {
		return 0
	}
	return s.duck.Begin(ctx)
}

func (s *Server) endDuck(ctx context.Context, gen int) {
	if s == nil || s.duck == nil || gen == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.duck.End(ctx, gen)
}

func (s *Server) holdDuck(ctx context.Context, gen int, after time.Duration, done func()) {
	if s == nil || s.duck == nil || gen == 0 {
		if done != nil {
			done()
		}
		return
	}
	time.AfterFunc(after, func() {
		s.endDuck(ctx, gen)
		if done != nil {
			done()
		}
	})
}

// chatCompletions is the Quasar Bridge endpoint. Tools run here, so the column only hears the final sentence.
func (s *Server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "messages required"})
		return
	}
	var turns []domain.Message
	for _, msg := range body.Messages {
		turns = append(turns, domain.Message{Role: msg.Role, Content: msg.Content})
	}
	prior, text := splitChat(turns)
	if strings.TrimSpace(text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "messages required"})
		return
	}
	ctx, scope := usecase.OpenCommand(context.WithoutCancel(r.Context()), "http", s.commands)
	defer scope.Finish()
	reply, err := s.utt.HandleChat(ctx, prior, text)
	if errors.Is(err, domain.ErrEmpty) {
		writeJSON(w, http.StatusOK, chatChoice(domain.EchoReply))
		return
	}
	if err != nil && reply.Text == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	content := strings.TrimSpace(reply.Text)
	if reply.Quiet {
		content = domain.SilentReply
	} else if content == "" {
		content = "Готово."
	}
	writeJSON(w, http.StatusOK, chatChoice(content))
}

func chatChoice(content string) map[string]any {
	return map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]string{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
	}
}

// splitChat keeps user and assistant turns and separates the latest user line.
func splitChat(in []domain.Message) (prior []domain.Message, latest string) {
	var turns []domain.Message
	for _, m := range in {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		turns = append(turns, domain.Message{Role: role, Content: strings.TrimSpace(m.Content)})
	}
	idx := -1
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "user" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, ""
	}
	return turns[:idx], turns[idx].Content
}

// speak synthesizes a phrase and returns a URL the column can play.
func (s *Server) speak(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text  string `json:"text"`
		Voice string `json:"voice"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty text"})
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty text"})
		return
	}
	ctx, scope := usecase.OpenCommand(context.WithoutCancel(r.Context()), "http", s.commands)
	gen := s.startDuck(ctx)
	audio, err := s.utt.Speak(ctx, body.Text)
	if err != nil {
		s.endDuck(ctx, gen)
		scope.Finish()
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	// The column downloads the file after this response, and the player puts its
	// gain back as soon as the dialog state goes idle. Hold the duck through both.
	hold := domain.WAVDuration(audio.Bytes) + 4*time.Second
	s.utt.HoldSpeech(hold)
	if mp3, convErr := wavToMP3(audio.Bytes, s.voice); convErr == nil {
		audio.Bytes = mp3
		audio.MIME = "audio/mpeg"
	}
	s.holdDuck(ctx, gen, hold, scope.Finish)
	url, err := s.saveAudio(audio)
	if err != nil {
		scope.SetOutcome(err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url, "voice": audio.Voice, "engine": "silero"})
}

func wavToMP3(wav []byte, volume int) ([]byte, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, err
	}
	in, err := os.CreateTemp("", "varya-*.wav")
	if err != nil {
		return nil, err
	}
	inName := in.Name()
	defer os.Remove(inName)
	if _, err := in.Write(wav); err != nil {
		_ = in.Close()
		return nil, err
	}
	if err := in.Close(); err != nil {
		return nil, err
	}
	outName := strings.TrimSuffix(inName, ".wav") + ".mp3"
	defer os.Remove(outName)
	// The file is full scale. Loudness is the column volume in the core, applied
	// once at the mixer. A second gain here would make the voice disagree with music.
	_ = volume
	args := []string{"-y", "-loglevel", "error", "-i", inName, "-codec:a", "libmp3lame", "-qscale:a", "4", outName}
	cmd := exec.Command("ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, errors.New(strings.TrimSpace(string(out)))
	}
	return os.ReadFile(outName)
}

func (s *Server) audio(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.audioDir, name)
	http.ServeFile(w, r, path)
}

func (s *Server) saveAudio(audio domain.Audio) (string, error) {
	if err := os.MkdirAll(s.audioDir, 0o755); err != nil {
		return "", err
	}
	ext := ".wav"
	if strings.Contains(audio.MIME, "mpeg") {
		ext = ".mp3"
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	name := hex.EncodeToString(b[:]) + ext
	if err := os.WriteFile(filepath.Join(s.audioDir, name), audio.Bytes, 0o644); err != nil {
		return "", err
	}
	return s.publicBase + "/audio/" + name, nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
