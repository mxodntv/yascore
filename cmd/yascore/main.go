package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"yascore/config"
	"yascore/internal/domain"
	"yascore/internal/music"
	"yascore/internal/repo"
	"yascore/internal/server"
	"yascore/internal/skillstate"
	"yascore/internal/usecase"
	"yascore/skills/mood"
)

func main() {
	configPath := flag.String("config", "config.yml", "path to yaml config")
	text := flag.String("text", "", "one-shot utterance, then exit")
	printConfig := flag.Bool("print", false, "print the loaded config as JSON and exit")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "скопируйте config.yml.example в config.yml")
		os.Exit(1)
	}
	on, err := config.Validate(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *printConfig {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	domain.Configure(cfg.Core.Duck.Percent)
	usecase.ConfigureDialogue(cfg.Core.Dialogue.Keep, cfg.Core.Dialogue.ToolRounds, cfg.Core.Dialogue.TTL)
	usecase.ConfigureHold(cfg.Core.Hold.ResumeQuiet, cfg.Core.Hold.ResumeAfterQuestion)
	mood.Install(cfg.Skills.Mood.Tones)

	hub := repo.NewHub()
	var player *repo.MusicPlayer
	var musicDB *music.Store
	if on.MusicYouTube {
		var err error
		musicDB, err = music.Open(cfg.Skills.MusicYouTube.Database)
		if err != nil {
			config.Error("music database", "err", err)
			os.Exit(1)
		}
		defer musicDB.Close()
		player = repo.NewMusicPlayer(repo.NewColumn(
			cfg.Skills.MusicYouTube.Host,
			cfg.Skills.MusicYouTube.Port,
			cfg.Skills.MusicYouTube.User,
			cfg.Skills.MusicYouTube.Bin,
		), cfg.Skills.MusicYouTube.ServiceURL)
		player.UseMemory(music.PlayerMemory{Store: musicDB})
		hub.UsePlayer(player)
	}
	var journal domain.TurnLogger
	var commands domain.CommandLogger
	if config.DebugEnabled() {
		opened, err := repo.NewConversationLog(cfg.Logs.Conversations)
		if err != nil {
			config.Error("conversation log", "err", err)
			os.Exit(1)
		}
		defer opened.Close()
		journal = opened
		openedCommands, err := repo.NewCommandLog(cfg.Logs.Commands)
		if err != nil {
			config.Error("command log", "err", err)
			os.Exit(1)
		}
		defer openedCommands.Close()
		commands = openedCommands
	}
	tts := repo.NewCachedTTS(
		repo.NewHTTPTTS(cfg.Core.TTS.URL, cfg.Core.TTS.Voice, cfg.Core.TTS.SampleRate, seconds(cfg.Core.TTS.TimeoutSec)),
		cfg.Core.TTS.CacheDir,
		cfg.Core.TTS.Voice,
	)
	utt := usecase.New(usecase.Deps{
		LLM:         repo.NewOllama(cfg.Core.LLM.BaseURL, cfg.Core.LLM.Model, seconds(cfg.Core.LLM.TimeoutSec)),
		Wake:        repo.NewTextWake(cfg.Core.Wake.Words),
		TTS:         tts,
		Skills:      hub,
		Prompt:      cfg.Core.Prompt,
		MoodDir:     cfg.Skills.Mood.Database,
		Voice:       cfg.Core.TTS.Voice,
		Model:       cfg.Core.LLM.Model,
		MaxTokens:   cfg.Core.LLM.MaxTokens,
		Temperature: cfg.Core.LLM.Temperature,
		Log:         journal,
		Commands:    commands,
	})
	audio := repo.NewPulse(cfg.Core.Audio.Input, cfg.Core.Audio.Output)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *text != "" {
		reply, spoken, err := utt.Answer(ctx, *text)
		if reply.Text != "" {
			fmt.Println(reply.Text)
		}
		if len(spoken.Bytes) > 0 {
			if playErr := audio.Play(ctx, spoken); playErr != nil {
				config.Error("play", "err", playErr)
			}
		}
		if err != nil {
			config.Error("answer", "err", err)
			os.Exit(1)
		}
		return
	}

	ln, err := net.Listen("tcp", cfg.Core.Server.Listen)
	if err != nil {
		config.Error("listen", "err", err)
		os.Exit(1)
	}
	edge := server.New(utt, hub, server.Info{
		Assistant:   cfg.Core.Name,
		LLM:         cfg.Core.LLM.Model,
		STT:         cfg.Core.STT.Engine,
		TTS:         cfg.Core.TTS.Engine,
		Skills:      hub.Count,
		VoiceVolume: cfg.Core.TTS.Volume,
	}, cfg.Core.Server.AudioDir, cfg.Core.Server.PublicBase)
	edge.UseDucking(usecase.NewDucker(hub))
	edge.UseCommands(commands)
	srv := &http.Server{Handler: edge.Handler()}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	config.Info("listening", "name", cfg.Core.Name, "listen", cfg.Core.Server.Listen, "model", cfg.Core.LLM.Model, "voice", cfg.Core.TTS.Voice)
	if err := skillstate.MigrateShared("."); err != nil {
		config.Error("migrate", "err", err)
		os.Exit(1)
	}
	registerSkills(ctx, cfg, on, musicDB, hub.Count)
	if player != nil {
		go player.StartWatch(ctx)
	}

	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			config.Error("server", "err", err)
			os.Exit(1)
		}
	}
}

func seconds(v float64) time.Duration {
	return time.Duration(v * float64(time.Second))
}
