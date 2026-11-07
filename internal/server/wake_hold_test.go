package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"yascore/internal/domain"
	"yascore/internal/repo"
	"yascore/internal/server"
	"yascore/internal/usecase"
)

type wakeHoldLLM struct {
	mu     sync.Mutex
	calls  int
	paused bool
	hub    *repo.Hub
	log    *wakeSkillLog
	text   string
}

func (l *wakeHoldLLM) Complete(context.Context, domain.CompleteRequest) (domain.Completion, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.log != nil && l.log.pauseCount() > 0 {
		l.paused = true
	}
	return domain.Completion{Text: l.text, Model: "fake"}, nil
}

type wakeSkillLog struct {
	mu       sync.Mutex
	pauses   int
	resumes  []int
	pauseAt  time.Time
	resumeAt time.Time
}

func (l *wakeSkillLog) pauseCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pauses
}

func (l *wakeSkillLog) snapshot() (int, []int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pauses, append([]int(nil), l.resumes...)
}

type configuredWake struct{ words []string }

func (w configuredWake) Strip(text string) string { return domain.Strip(text, w.words) }
func (w configuredWake) Words() []string {
	return append([]string(nil), w.words...)
}

// TestUtteranceWakePausesMediaBeforeLLM is the integration-style check:
// POST /v1/utterance with the wake word must pause the registered media skill
// before the model returns, then resume the saved position after the quiet window.
func TestUtteranceWakePausesMediaBeforeLLM(t *testing.T) {
	name, words := exampleAssistant(t)
	hub := repo.NewHub()
	log := &wakeSkillLog{}
	model := &wakeHoldLLM{hub: hub, log: log, text: "Нормально."}
	u := usecase.New(usecase.Deps{
		LLM: model, Wake: configuredWake{words: words}, Skills: hub, Prompt: "p",
	})
	u.SetHoldQuiet(40 * time.Millisecond)
	edge := server.New(u, hub, server.Info{Assistant: name}, t.TempDir(), "http://127.0.0.1:8091")
	ts := httptest.NewServer(edge.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/v1/skills", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	reg, _ := json.Marshal(domain.BusMessage{
		Type: "register", Name: "youtubemusic", Sound: true, Media: true,
		Tools: []domain.Tool{{Name: "play_music"}},
	})
	if err := conn.Write(ctx, websocket.MessageText, reg); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	go serveWakeSkill(ctx, conn, log)

	payload, err := json.Marshal(map[string]string{"text": name + ", как дела"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/v1/utterance", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("utterance %s %s", resp.Status, raw)
	}
	model.mu.Lock()
	calls, paused := model.calls, model.paused
	model.mu.Unlock()
	if calls != 1 || !paused {
		t.Fatalf("media pause must happen before the LLM: calls=%d paused=%v body=%s", calls, paused, raw)
	}
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		pauses, resumes := log.snapshot()
		if pauses >= 1 && len(resumes) == 1 && resumes[0] == 95025 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	pauses, resumes := log.snapshot()
	t.Fatalf("pause=%d resumes=%v want resume 95025", pauses, resumes)
}

func TestUtteranceWakeSecondCommandCancelsFirstResume(t *testing.T) {
	name, words := exampleAssistant(t)
	hub := repo.NewHub()
	log := &wakeSkillLog{}
	model := &wakeHoldLLM{hub: hub, log: log, text: "Ок."}
	u := usecase.New(usecase.Deps{
		LLM: model, Wake: configuredWake{words: words}, Skills: hub, Prompt: "p",
	})
	u.SetHoldQuiet(100 * time.Millisecond)
	edge := server.New(u, hub, server.Info{Assistant: name}, t.TempDir(), "http://127.0.0.1:8091")
	ts := httptest.NewServer(edge.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/v1/skills", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	reg, _ := json.Marshal(domain.BusMessage{
		Type: "register", Name: "youtubemusic", Sound: true, Media: true,
		Tools: []domain.Tool{{Name: "play_music"}},
	})
	if err := conn.Write(ctx, websocket.MessageText, reg); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	go serveWakeSkill(ctx, conn, log)

	post := func(text string) {
		t.Helper()
		body := `{"text":` + jsonString(text) + `}`
		resp, err := http.Post(ts.URL+"/v1/utterance", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("utterance %s", resp.Status)
		}
	}
	post(name + ", как дела")
	time.Sleep(30 * time.Millisecond)
	if _, resumes := log.snapshot(); len(resumes) != 0 {
		t.Fatalf("resumed too early: %v", resumes)
	}
	post(name + ", ещё")
	time.Sleep(60 * time.Millisecond)
	if pauses, resumes := log.snapshot(); pauses < 2 || len(resumes) != 0 {
		t.Fatalf("first resume must be cancelled: pauses=%d resumes=%v", pauses, resumes)
	}
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, resumes := log.snapshot()
		if len(resumes) == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, resumes := log.snapshot()
	t.Fatalf("second quiet window should resume once, got %v", resumes)
}

func TestBarePlayerStopWithoutWake(t *testing.T) {
	name, words := exampleAssistant(t)
	hub := repo.NewHub()
	log := &wakeSkillLog{}
	model := &wakeHoldLLM{hub: hub, log: log, text: "нет"}
	u := usecase.New(usecase.Deps{
		LLM: model, Wake: configuredWake{words: words}, Skills: hub, Prompt: "p",
	})
	edge := server.New(u, hub, server.Info{Assistant: name}, t.TempDir(), "http://127.0.0.1:8091")
	ts := httptest.NewServer(edge.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/v1/skills", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	reg, _ := json.Marshal(domain.BusMessage{
		Type: "register", Name: "youtubemusic", Sound: true, Media: true,
		Tools: []domain.Tool{{Name: "music_control"}, {Name: "play_music"}},
	})
	if err := conn.Write(ctx, websocket.MessageText, reg); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	var invokes int
	var mu sync.Mutex
	go func() {
		for ctx.Err() == nil {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg domain.BusMessage
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			content := "ok"
			switch msg.Type {
			case "pause":
				log.mu.Lock()
				log.pauses++
				log.mu.Unlock()
				content = `{"ok":true,"position_ms":1}`
			case "resume":
				log.mu.Lock()
				log.resumes = append(log.resumes, resumePos(msg.Arguments))
				log.mu.Unlock()
			case "invoke":
				mu.Lock()
				invokes++
				mu.Unlock()
				content = domain.SilentReply
			}
			if msg.ID == "" {
				continue
			}
			out, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: content})
			_ = conn.Write(ctx, websocket.MessageText, out)
		}
	}()

	resp, err := http.Post(ts.URL+"/v1/utterance", "application/json", strings.NewReader(`{"text":"стоп"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var body struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &body)
	model.mu.Lock()
	calls := model.calls
	model.mu.Unlock()
	mu.Lock()
	gotInvokes := invokes
	mu.Unlock()
	pauses, resumes := log.snapshot()
	if resp.StatusCode != http.StatusOK || body.Text != domain.StoppedReply || calls != 0 {
		t.Fatalf("bare stop status=%d text=%q llm=%d body=%s", resp.StatusCode, body.Text, calls, raw)
	}
	if pauses == 0 || len(resumes) != 0 || gotInvokes != 0 {
		t.Fatalf("bare stop pause=%d resumes=%v invokes=%d", pauses, resumes, gotInvokes)
	}
}

func serveWakeSkill(ctx context.Context, conn *websocket.Conn, log *wakeSkillLog) {
	for ctx.Err() == nil {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg domain.BusMessage
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		content := "ok"
		switch msg.Type {
		case "pause":
			log.mu.Lock()
			log.pauses++
			if log.pauseAt.IsZero() {
				log.pauseAt = time.Now()
			}
			log.mu.Unlock()
			content = `{"ok":true,"position_ms":95025}`
		case "resume":
			pos := resumePos(msg.Arguments)
			log.mu.Lock()
			log.resumes = append(log.resumes, pos)
			log.resumeAt = time.Now()
			log.mu.Unlock()
			content = domain.SilentReply
		}
		if msg.ID == "" {
			continue
		}
		out, _ := json.Marshal(domain.BusMessage{Type: "result", ID: msg.ID, Content: content})
		_ = conn.Write(ctx, websocket.MessageText, out)
	}
}

func resumePos(args string) int {
	var parsed struct {
		PositionMS *int `json:"position_ms"`
	}
	if json.Unmarshal([]byte(args), &parsed) != nil || parsed.PositionMS == nil {
		return -1
	}
	return *parsed.PositionMS
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
