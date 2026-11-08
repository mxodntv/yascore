package main

import (
	"context"
	"time"

	"yascore/config"
	"yascore/internal/music"
	"yascore/skills/currency"
	"yascore/skills/mood"
	"yascore/skills/rutube"
	"yascore/skills/smarthome"
	"yascore/skills/vkvideo"
	"yascore/skills/weather"
	"yascore/skills/websearch"
	"yascore/skills/youtube"
    "yascore/skills/musicyoutube"
)

type skillReg struct {
	name string
	fn   func(context.Context, string) error
}

func skillRegs(cfg config.Config, on config.SkillSet, store *music.Store) []skillReg {
	regs := []skillReg{
		{"youtube", youtube.Register},
		{"rutube", rutube.Register},
		{"vkvideo", vkvideo.Register},
		{"smarthome", smarthome.Register},
	}
	if on.WebSearch {
		search := cfg.Skills.WebSearch
		regs = append(regs, skillReg{"websearch", func(ctx context.Context, url string) error {
			return websearch.Register(ctx, url, search)
		}})
	}
	if on.MusicYouTube {
		musicCfg := cfg.Skills.MusicYouTube
		regs = append(regs, skillReg{"musicyoutube", func(ctx context.Context, url string) error {
			return musicyoutube.Register(ctx, url, musicCfg, store)
		}})
	}
	if on.Mood {
		tone := cfg.Skills.Mood
		regs = append(regs, skillReg{"mood", func(ctx context.Context, url string) error {
			return mood.Register(ctx, url, tone)
		}})
	}
	if on.Weather {
		w := cfg.Skills.Weather
		regs = append(regs, skillReg{"weather", func(ctx context.Context, url string) error {
			return weather.Register(ctx, url, w)
		}})
	}
	if on.Currency {
		c := cfg.Skills.Currency
		regs = append(regs, skillReg{"currency", func(ctx context.Context, url string) error {
			return currency.Register(ctx, url, c)
		}})
	}
	return regs
}

// registerSkills dials the hub. Call it only after the HTTP server is listening.
func registerSkills(ctx context.Context, cfg config.Config, on config.SkillSet, store *music.Store, connected func() int) {
	regs := skillRegs(cfg, on, store)
	for _, reg := range regs {
		reg := reg
		go func() {
			if err := reg.fn(ctx, cfg.Core.SkillsSocket); err != nil && ctx.Err() == nil {
				config.Error("skill", "name", reg.name, "err", err)
			}
		}()
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if connected() >= len(regs) {
			config.Info("skills registered", "count", connected())
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			config.Info("skills registered", "count", connected(), "want", len(regs))
			return
		case <-tick.C:
		}
	}
}
