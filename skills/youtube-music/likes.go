package youtubemusic

import (
	"context"
	"fmt"
	"strings"
	"time"

	"yascore/internal/domain"
)

const (
	noLikesYet    = domain.NoLikesYet
	nothingToLike = domain.NothingToLike
	notInLikes    = domain.NotInLikes
	likedOK       = domain.LikedReply
	unlikedOK     = domain.UnlikedReply
)

// savedTrack is one liked or recently played track.
type savedTrack struct {
	ID      string `json:"id,omitempty"`
	Title   string `json:"title"`
	Artists string `json:"artists,omitempty"`
}

type musicState struct {
	Likes  []savedTrack `json:"likes"`
	Recent *savedTrack  `json:"recent,omitempty"`
	Tracks []savedTrack `json:"tracks,omitempty"`
	Index  int          `json:"index,omitempty"`
}

func (s Speaker) likeIntent(ctx context.Context, title, artists, id string) (string, error) {
	track, ok := s.target(title, artists, id)
	if !ok {
		return sayIntent(nothingToLike)
	}
	if s.music == nil {
		return "", fmt.Errorf("лайки не сохраняются")
	}
	now := time.Now().UTC().Truncate(time.Second)
	last := now
	if played, found, err := s.music.LastPlayed(track.Title, track.Artists, ServiceYouTube); err != nil {
		return "", err
	} else if found {
		last = played
	}
	if err := s.music.Like(track.Title, track.Artists, track.ID, ServiceYouTube, now, last); err != nil {
		return "", err
	}
	return sayIntent(likedOK)
}

func (s Speaker) unlikeIntent(ctx context.Context, title, artists, id string) (string, error) {
	if s.music == nil {
		return "", fmt.Errorf("лайки не сохраняются")
	}
	likes, err := s.music.Likes()
	if err != nil {
		return "", err
	}
	if len(likes) == 0 {
		return sayIntent(noLikesYet)
	}
	track, ok := s.target(title, artists, id)
	if !ok {
		return sayIntent(nothingToLike)
	}
	if !containsTrack(likesToSaved(likes), track) {
		return sayIntent(notInLikes)
	}
	removed, err := s.music.Unlike(track.Title, track.Artists, track.ID, ServiceYouTube)
	if err != nil {
		return "", err
	}
	if !removed {
		return sayIntent(notInLikes)
	}
	return sayIntent(unlikedOK)
}

func (s Speaker) playLikesIntent(ctx context.Context) (string, error) {
	state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	if len(state.Likes) == 0 {
		return sayIntent(noLikesYet)
	}
	track := state.Likes[0]
	query := playbackName(track)
	if query == "" {
		query = playbackQuery(track)
	}
	if query == "" {
		return sayIntent(noLikesYet)
	}
	return intentJSON(domain.MusicIntent{Do: domain.MusicSearchPlay, Query: query})
}

func (s Speaker) playSimilarIntent(ctx context.Context) (string, error) {
	state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	if len(state.Likes) == 0 {
		return sayIntent(noLikesYet)
	}
	track := state.Likes[0]
	query := strings.TrimSpace(track.Artists)
	if query == "" {
		query = playbackName(track)
	}
	if query == "" {
		return sayIntent(noLikesYet)
	}
	return intentJSON(domain.MusicIntent{Do: domain.MusicSearchPlay, Query: query})
}

func (s Speaker) target(title, artists, id string) (savedTrack, bool) {
	title = strings.TrimSpace(title)
	artists = strings.TrimSpace(artists)
	id = strings.TrimSpace(id)
	if title != "" || id != "" {
		return savedTrack{ID: id, Title: title, Artists: artists}, true
	}
	state, err := s.load(context.Background())
	if err != nil || state.Recent == nil {
		return savedTrack{}, false
	}
	track := *state.Recent
	if strings.TrimSpace(track.ID) == "" && strings.TrimSpace(track.Title) == "" {
		return savedTrack{}, false
	}
	return track, true
}

func (s Speaker) load(context.Context) (musicState, error) {
	state := musicState{Likes: []savedTrack{}}
	if s.music == nil {
		return state, fmt.Errorf("лайки не сохраняются")
	}
	snap, err := s.music.Load()
	if err != nil {
		return musicState{}, err
	}
	state.Likes = fromTracks(snap.Likes)
	state.Tracks = fromTracks(snap.Plays)
	state.Index = snap.Index
	if snap.Recent != nil {
		recent := fromTrack(*snap.Recent)
		state.Recent = &recent
	}
	return state, nil
}

func (s Speaker) save(_ context.Context, state musicState) error {
	if s.music == nil {
		return fmt.Errorf("лайки не сохраняются")
	}
	return s.music.Save(toSnapshot(state))
}

func toSnapshot(state musicState) Snapshot {
	snap := Snapshot{
		Plays: toTracks(state.Tracks),
		Index: state.Index,
	}
	if state.Likes != nil {
		snap.Likes = toTracks(state.Likes)
	}
	if state.Recent != nil {
		recent := toTrack(*state.Recent)
		snap.Recent = &recent
	}
	return snap
}

func toTracks(list []savedTrack) []Track {
	if list == nil {
		return nil
	}
	out := make([]Track, 0, len(list))
	for _, item := range list {
		out = append(out, toTrack(item))
	}
	return out
}

func toTrack(item savedTrack) Track {
	return Track{
		VideoID: item.ID,
		Title:   item.Title,
		Artist:  item.Artists,
		Service: ServiceYouTube,
	}
}

func fromTracks(list []Track) []savedTrack {
	out := make([]savedTrack, 0, len(list))
	for _, item := range list {
		out = append(out, fromTrack(item))
	}
	return out
}

func fromTrack(item Track) savedTrack {
	return savedTrack{ID: item.VideoID, Title: item.Title, Artists: item.Artist}
}

func likesToSaved(likes []Like) []savedTrack {
	out := make([]savedTrack, 0, len(likes))
	for _, item := range likes {
		out = append(out, savedTrack{ID: item.VideoID, Title: item.Title, Artists: item.Artist})
	}
	return out
}

func containsTrack(list []savedTrack, track savedTrack) bool {
	for _, item := range list {
		if sameTrack(item, track) {
			return true
		}
	}
	return false
}

func sameTrack(a, b savedTrack) bool {
	if a.ID != "" && a.ID == b.ID {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(a.Title), strings.TrimSpace(b.Title)) &&
		strings.EqualFold(strings.TrimSpace(a.Artists), strings.TrimSpace(b.Artists))
}

func playbackQuery(track savedTrack) string {
	if strings.TrimSpace(track.ID) != "" {
		return serviceBase + "/watch?v=" + strings.TrimSpace(track.ID)
	}
	return playbackName(track)
}

func playbackName(track savedTrack) string {
	title := strings.TrimSpace(track.Title)
	artist := strings.TrimSpace(track.Artists)
	if title == "" {
		return artist
	}
	if artist == "" {
		return title
	}
	return title + " " + artist
}
