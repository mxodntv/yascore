package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Validate checks the loaded config. A missing core host or path is an error.
// A missing skill host is a warning and that skill stays off.
// A skill host that is set but not an http URL is an error.
func Validate(c Config) (SkillSet, error) {
	if err := SetLevel(c.Logs.Level); err != nil {
		return SkillSet{}, fmt.Errorf("config: %w", err)
	}
	var errs []string
	need := func(cond bool, msg string) {
		if cond {
			errs = append(errs, msg)
		}
	}
	need(strings.TrimSpace(c.Core.Name) == "", "core.name is empty")
	need(strings.TrimSpace(c.Core.Prompt) == "", "core.prompt is empty")
	need(strings.TrimSpace(c.Core.BridgePrompt) == "", "core.bridge_prompt is empty")
	need(len(nonEmpty(c.Core.Wake.Words)) == 0, "core.wake.words is empty")
	need(c.Core.STT.Engine != "faster-whisper", "core.stt.engine must be faster-whisper")
	need(c.Core.LLM.Engine != "ollama", "core.llm.engine must be ollama")
	need(c.Core.TTS.Engine != "silero", "core.tts.engine must be silero")
	if err := needHTTP(c.Core.STT.URL); err != nil {
		errs = append(errs, "core.stt.url: "+err.Error())
	}
	need(strings.TrimSpace(c.Core.STT.Model) == "", "core.stt.model is empty")
	need(c.Core.STT.TimeoutSec <= 0, "core.stt.timeout_sec must be positive")
	need(strings.TrimSpace(c.Core.STT.Language) == "", "core.stt.language is empty")
	if err := needHTTP(c.Core.LLM.BaseURL); err != nil {
		errs = append(errs, "core.llm.base_url: "+err.Error())
	}
	need(strings.TrimSpace(c.Core.LLM.Model) == "", "core.llm.model is empty")
	need(c.Core.LLM.TimeoutSec <= 0, "core.llm.timeout_sec must be positive")
	need(c.Core.LLM.MaxTokens <= 0, "core.llm.max_tokens must be positive")
	if err := needHTTP(c.Core.TTS.URL); err != nil {
		errs = append(errs, "core.tts.url: "+err.Error())
	}
	need(strings.TrimSpace(c.Core.TTS.Voice) == "", "core.tts.voice is empty")
	need(c.Core.TTS.Volume < 1 || c.Core.TTS.Volume > 100, "core.tts.volume must be 1..100")
	need(c.Core.TTS.SampleRate <= 0, "core.tts.sample_rate must be positive")
	need(c.Core.TTS.TimeoutSec <= 0, "core.tts.timeout_sec must be positive")
	need(strings.TrimSpace(c.Core.TTS.CacheDir) == "", "core.tts.cache_dir is empty")
	need(strings.TrimSpace(c.Core.Audio.Input) == "", "core.audio.input is empty")
	need(strings.TrimSpace(c.Core.Audio.Output) == "", "core.audio.output is empty")
	if _, _, err := net.SplitHostPort(strings.TrimSpace(c.Core.Server.Listen)); err != nil {
		errs = append(errs, "core.server.listen must be host:port")
	}
	if err := needHTTP(c.Core.Server.PublicBase); err != nil {
		errs = append(errs, "core.server.public_base: "+err.Error())
	}
	need(strings.TrimSpace(c.Core.Server.AudioDir) == "", "core.server.audio_dir is empty")
	if err := needURL(c.Core.SkillsSocket, "ws", "wss"); err != nil {
		errs = append(errs, "core.skills_socket: "+err.Error())
	}
	need(c.Core.Dialogue.Keep <= 0, "core.dialogue.keep must be positive")
	need(c.Core.Dialogue.ToolRounds <= 0, "core.dialogue.tool_rounds must be positive")
	need(c.Core.Dialogue.TTL <= 0, "core.dialogue.ttl must be positive")
	need(c.Core.Hold.ResumeQuiet <= 0, "core.hold.resume_quiet must be positive")
	need(c.Core.Hold.ResumeAfterQuestion <= 0, "core.hold.resume_after_question must be positive")
	need(c.Core.Duck.Percent < 1 || c.Core.Duck.Percent > 100, "core.duck.percent must be 1..100")
	need(strings.TrimSpace(c.Logs.Conversations) == "", "logs.conversations is empty")
	need(strings.TrimSpace(c.Logs.Commands) == "", "logs.commands is empty")
	need(strings.TrimSpace(c.Logs.Audio) == "", "logs.audio is empty")

	var on SkillSet
	on.Weather = skillURL(&errs, "skills.weather.api_base", c.Skills.Weather.APIBase) && skillURL(&errs, "skills.weather.geocode_base", c.Skills.Weather.GeocodeBase)
	if on.Weather {
		need(strings.TrimSpace(c.Skills.Weather.Provider) == "", "skills.weather.provider is empty")
		need(strings.TrimSpace(c.Skills.Weather.Database) == "", "skills.weather.database is empty")
		need(c.Skills.Weather.TimeoutSec <= 0, "skills.weather.timeout_sec must be positive")
	}
	on.Currency = skillURL(&errs, "skills.currency.api_base", c.Skills.Currency.APIBase)
	if on.Currency {
		need(strings.TrimSpace(c.Skills.Currency.Database) == "", "skills.currency.database is empty")
		need(c.Skills.Currency.TimeoutSec <= 0, "skills.currency.timeout_sec must be positive")
	}
	on.WebSearch = skillURL(&errs, "skills.websearch.endpoint", c.Skills.WebSearch.Endpoint)
	if on.WebSearch {
		need(c.Skills.WebSearch.TimeoutSec <= 0, "skills.websearch.timeout_sec must be positive")
	}
	on.Mood = strings.TrimSpace(c.Skills.Mood.Database) != "" && len(c.Skills.Mood.Tones) > 0
	if !on.Mood {
		Warn("skill off", "skill", "mood", "reason", "database or tones empty")
	}
	on.MusicYouTube = youtubeOn(&errs, c.Skills.MusicYouTube)
	if on.MusicYouTube {
		need(c.Skills.MusicYouTube.Port <= 0, "skills.musicyoutube.port must be positive")
		need(strings.TrimSpace(c.Skills.MusicYouTube.User) == "", "skills.musicyoutube.user is empty")
		need(strings.TrimSpace(c.Skills.MusicYouTube.Bin) == "", "skills.musicyoutube.bin is empty")
		need(strings.TrimSpace(c.Skills.MusicYouTube.Database) == "", "skills.musicyoutube.database is empty")
	}
	if len(errs) == 0 {
		return on, nil
	}
	return SkillSet{}, fmt.Errorf("config: %s", strings.Join(errs, "; "))
}

// skillURL warns and returns false when the host is empty.
// A non-empty value that is not an http URL is an error.
func skillURL(errs *[]string, name, raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		Warn("skill off", "skill", name, "reason", "host empty")
		return false
	}
	if err := needHTTP(raw); err != nil {
		*errs = append(*errs, name+": "+err.Error())
		return false
	}
	return true
}

func youtubeOn(errs *[]string, y MusicYouTube) bool {
	if strings.TrimSpace(y.Host) == "" || strings.TrimSpace(y.ServiceURL) == "" {
		Warn("skill off", "skill", "skills.musicyoutube", "reason", "host empty")
		return false
	}
	if err := needHTTP(y.ServiceURL); err != nil {
		*errs = append(*errs, "skills.musicyoutube.service_url: "+err.Error())
		return false
	}
	return true
}

func needHTTP(raw string) error {
	return needURL(raw, "http", "https")
}

func needURL(raw string, schemes ...string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.Scheme == "" {
		return fmt.Errorf("invalid url %q", raw)
	}
	for _, s := range schemes {
		if u.Scheme == s {
			return nil
		}
	}
	return fmt.Errorf("scheme %q not allowed", u.Scheme)
}
