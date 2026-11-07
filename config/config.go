package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the process configuration. Every prompt, host, name, and path lives here.
type Config struct {
	Core   Core   `yaml:"core" json:"core"`
	Skills Skills `yaml:"skills" json:"skills"`
	Logs   Logs   `yaml:"logs" json:"logs"`
}

// Core is what the process needs in order to answer.
type Core struct {
	Name         string   `yaml:"name" json:"name"`
	Prompt       string   `yaml:"prompt" json:"prompt"`
	BridgePrompt string   `yaml:"bridge_prompt" json:"bridge_prompt"`
	Wake         Wake     `yaml:"wake" json:"wake"`
	STT          STT      `yaml:"stt" json:"stt"`
	LLM          LLM      `yaml:"llm" json:"llm"`
	TTS          TTS      `yaml:"tts" json:"tts"`
	Audio        Audio    `yaml:"audio" json:"audio"`
	Server       Server   `yaml:"server" json:"server"`
	SkillsSocket string   `yaml:"skills_socket" json:"skills_socket"`
	Dialogue     Dialogue `yaml:"dialogue" json:"dialogue"`
	Hold         Hold     `yaml:"hold" json:"hold"`
	Duck         Duck     `yaml:"duck" json:"duck"`
}

// Wake lists the words stripped from the front of a phrase.
type Wake struct {
	Words []string `yaml:"words" json:"words"`
}

// STT is the speech-to-text worker.
type STT struct {
	Engine     string  `yaml:"engine" json:"engine"`
	URL        string  `yaml:"url" json:"url"`
	Model      string  `yaml:"model" json:"model"`
	Language   string  `yaml:"language" json:"language"`
	TimeoutSec float64 `yaml:"timeout_sec" json:"timeout_sec"`
	Prompt     string  `yaml:"prompt" json:"prompt"`
	Hotwords   string  `yaml:"hotwords" json:"hotwords"`
}

// LLM is the chat model.
type LLM struct {
	Engine      string  `yaml:"engine" json:"engine"`
	BaseURL     string  `yaml:"base_url" json:"base_url"`
	Model       string  `yaml:"model" json:"model"`
	TimeoutSec  float64 `yaml:"timeout_sec" json:"timeout_sec"`
	MaxTokens   int     `yaml:"max_tokens" json:"max_tokens"`
	Temperature float64 `yaml:"temperature" json:"temperature"`
}

// TTS is the speech worker and the files it needs.
type TTS struct {
	Engine     string  `yaml:"engine" json:"engine"`
	URL        string  `yaml:"url" json:"url"`
	Voice      string  `yaml:"voice" json:"voice"`
	Volume     int     `yaml:"volume" json:"volume"`
	SampleRate int     `yaml:"sample_rate" json:"sample_rate"`
	TimeoutSec float64 `yaml:"timeout_sec" json:"timeout_sec"`
	Model      string  `yaml:"model" json:"model"`
	Python     string  `yaml:"python" json:"python"`
	CLI        string  `yaml:"cli" json:"cli"`
	CacheDir   string  `yaml:"cache_dir" json:"cache_dir"`
}

// Audio names the local capture and playback devices.
type Audio struct {
	Input  string `yaml:"input" json:"input"`
	Output string `yaml:"output" json:"output"`
}

// Server is the HTTP edge. AudioDir holds the MP3 the speaker downloads.
type Server struct {
	Listen     string `yaml:"listen" json:"listen"`
	PublicBase string `yaml:"public_base" json:"public_base"`
	AudioDir   string `yaml:"audio_dir" json:"audio_dir"`
}

// Dialogue is how much of the conversation the model sees.
type Dialogue struct {
	Keep       int           `yaml:"keep" json:"keep"`
	TTLRaw     string        `yaml:"ttl" json:"ttl"`
	ToolRounds int           `yaml:"tool_rounds" json:"tool_rounds"`
	TTL        time.Duration `yaml:"-" json:"-"`
}

// Hold is how long playback stays paused after a reply.
type Hold struct {
	ResumeQuietRaw         string        `yaml:"resume_quiet" json:"resume_quiet"`
	ResumeAfterQuestionRaw string        `yaml:"resume_after_question" json:"resume_after_question"`
	ResumeQuiet            time.Duration `yaml:"-" json:"-"`
	ResumeAfterQuestion    time.Duration `yaml:"-" json:"-"`
}

// Duck is the percent of the current skill volume kept while the assistant speaks.
type Duck struct {
	Percent int `yaml:"percent" json:"percent"`
}

// Skills is every optional ability. An empty host leaves that skill off.
type Skills struct {
	Weather      Weather      `yaml:"weather" json:"weather"`
	Currency     Currency     `yaml:"currency" json:"currency"`
	WebSearch    WebSearch    `yaml:"websearch" json:"websearch"`
	Mood         Mood         `yaml:"mood" json:"mood"`
	YouTubeMusic YouTubeMusic `yaml:"youtube_music" json:"youtube_music"`
}

// Weather is the forecast skill.
type Weather struct {
	Provider    string  `yaml:"provider" json:"provider"`
	APIBase     string  `yaml:"api_base" json:"api_base"`
	GeocodeBase string  `yaml:"geocode_base" json:"geocode_base"`
	Database    string  `yaml:"database" json:"database"`
	TimeoutSec  float64 `yaml:"timeout_sec" json:"timeout_sec"`
}

// Currency is the exchange-rate skill.
type Currency struct {
	APIBase    string  `yaml:"api_base" json:"api_base"`
	Database   string  `yaml:"database" json:"database"`
	TimeoutSec float64 `yaml:"timeout_sec" json:"timeout_sec"`
}

// WebSearch is the HTML search skill.
type WebSearch struct {
	Endpoint   string  `yaml:"endpoint" json:"endpoint"`
	TimeoutSec float64 `yaml:"timeout_sec" json:"timeout_sec"`
}

// Mood is the tone skill. Tones are the prompt addenda.
type Mood struct {
	Database string     `yaml:"database" json:"database"`
	Tones    []MoodTone `yaml:"tones" json:"tones"`
}

// MoodTone is one named tone.
type MoodTone struct {
	ID     string   `yaml:"id" json:"id"`
	Will   string   `yaml:"will" json:"will"`
	Prompt string   `yaml:"prompt" json:"prompt"`
	Keys   []string `yaml:"keys" json:"keys"`
}

// YouTubeMusic is the column player skill and the paths the column scripts use.
type YouTubeMusic struct {
	Host         string `yaml:"host" json:"host"`
	Port         int    `yaml:"port" json:"port"`
	User         string `yaml:"user" json:"user"`
	Bin          string `yaml:"bin" json:"bin"`
	ServiceURL   string `yaml:"service_url" json:"service_url"`
	Database     string `yaml:"database" json:"database"`
	NoProxy      string `yaml:"no_proxy" json:"no_proxy"`
	BridgeConfig string `yaml:"bridge_config" json:"bridge_config"`
	BridgeBin    string `yaml:"bridge_bin" json:"bridge_bin"`
	BridgeSrc    string `yaml:"bridge_src" json:"bridge_src"`
	WakeFile     string `yaml:"wake_file" json:"wake_file"`
	ProxyEnv     string `yaml:"proxy_env" json:"proxy_env"`
	PlayerHome   string `yaml:"player_home" json:"player_home"`
	SpotterDir   string `yaml:"spotter_dir" json:"spotter_dir"`
	SpotterBind  string `yaml:"spotter_bind" json:"spotter_bind"`
}

// Logs selects the level and the journal paths.
type Logs struct {
	Level         string `yaml:"level" json:"level"`
	Conversations string `yaml:"conversations" json:"conversations"`
	Commands      string `yaml:"commands" json:"commands"`
	Audio         string `yaml:"audio" json:"audio"`
}

// SkillSet reports which skills Validate left enabled.
type SkillSet struct {
	Weather      bool
	Currency     bool
	WebSearch    bool
	Mood         bool
	YouTubeMusic bool
}

// Load reads config.yml, substitutes {name}, and parses durations.
// It does not fill missing hosts or paths.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.Core.Name = strings.TrimSpace(cfg.Core.Name)
	cfg.substituteName()
	cfg.Core.Wake.Words = nonEmpty(cfg.Core.Wake.Words)
	if len(cfg.Core.Wake.Words) == 0 && cfg.Core.Name != "" {
		cfg.Core.Wake.Words = []string{cfg.Core.Name}
	}
	if err := cfg.parseDurations(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) substituteName() {
	name := c.Core.Name
	repl := func(s string) string { return strings.ReplaceAll(s, "{name}", name) }
	c.Core.Prompt = repl(c.Core.Prompt)
	c.Core.BridgePrompt = repl(c.Core.BridgePrompt)
	c.Core.STT.Prompt = repl(c.Core.STT.Prompt)
	c.Core.STT.Hotwords = repl(c.Core.STT.Hotwords)
	for i := range c.Skills.Mood.Tones {
		c.Skills.Mood.Tones[i].Prompt = repl(c.Skills.Mood.Tones[i].Prompt)
	}
}

func (c *Config) parseDurations() error {
	var err error
	if c.Core.Dialogue.TTL, err = parseDuration("core.dialogue.ttl", c.Core.Dialogue.TTLRaw); err != nil {
		return err
	}
	if c.Core.Hold.ResumeQuiet, err = parseDuration("core.hold.resume_quiet", c.Core.Hold.ResumeQuietRaw); err != nil {
		return err
	}
	if c.Core.Hold.ResumeAfterQuestion, err = parseDuration("core.hold.resume_after_question", c.Core.Hold.ResumeAfterQuestionRaw); err != nil {
		return err
	}
	return nil
}

func parseDuration(field, raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s is empty", field)
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", field)
	}
	return d, nil
}

func nonEmpty(words []string) []string {
	var out []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}
