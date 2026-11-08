package musicyoutube

import (
	"context"
	"fmt"
	"strings"

	"yascore/internal/domain"
)

func (s *Speaker) previousIntent(ctx context.Context) (string, error) {
	if s.music == nil {
		return intentJSON(domain.MusicIntent{Do: domain.MusicControl, Action: "previous"})
	}
	state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	if state.Index <= 0 || state.Index >= len(state.Tracks) {
		return intentJSON(domain.MusicIntent{Do: domain.MusicControl, Action: "previous"})
	}
	return s.playAtIntent(ctx, state.Index-1)
}

func (s *Speaker) nextIntent(ctx context.Context) (string, error) {
	if s.music == nil {
		return intentJSON(domain.MusicIntent{Do: domain.MusicControl, Action: "next"})
	}
	state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	if state.Index < 0 || state.Index+1 >= len(state.Tracks) {
		return intentJSON(domain.MusicIntent{Do: domain.MusicControl, Action: "next"})
	}
	return s.playAtIntent(ctx, state.Index+1)
}

func (s *Speaker) advanceIntent(ctx context.Context) (string, error) {
	state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	next := state.Index + 1
	if state.Index < 0 || next >= len(state.Tracks) {
		return intentJSON(domain.MusicIntent{Do: domain.MusicSay, Text: domain.SilentReply})
	}
	return s.playAtIntent(ctx, next)
}

func (s *Speaker) playAtIntent(ctx context.Context, index int) (string, error) {
	state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	if index < 0 || index >= len(state.Tracks) {
		return "", fmt.Errorf("не поняла, какую песню включить")
	}
	line, err := s.storedPhrase(state.Tracks[index])
	if err != nil {
		return "", err
	}
	state.Index = index
	cur := state.Tracks[index]
	state.Recent = &cur
	if err := s.save(ctx, state); err != nil {
		return "", err
	}
	if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
		return intentJSON(domain.MusicIntent{Do: domain.MusicPlay, Phrase: line, Follow: true})
	}
	return intentJSON(domain.MusicIntent{Do: domain.MusicSearchPlay, Query: line, Follow: true})
}

func (s *Speaker) storedPhrase(track savedTrack) (string, error) {
	if name := playbackName(track); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("не поняла, какую песню включить")
}

// appendCurrent drops anything waiting after the current song and appends track.
func appendCurrent(state musicState, track savedTrack) musicState {
	if strings.TrimSpace(track.Title) == "" && strings.TrimSpace(track.ID) == "" {
		return state
	}
	if state.Index < 0 || state.Index >= len(state.Tracks) {
		if len(state.Tracks) == 0 {
			state.Index = -1
		} else {
			state.Index = len(state.Tracks) - 1
		}
	}
	if state.Index >= 0 && sameTrack(state.Tracks[state.Index], track) {
		return state
	}
	if state.Index >= 0 && state.Index < len(state.Tracks)-1 {
		state.Tracks = append([]savedTrack(nil), state.Tracks[:state.Index+1]...)
	}
	state.Tracks = append(state.Tracks, track)
	state.Index = len(state.Tracks) - 1
	return state
}
