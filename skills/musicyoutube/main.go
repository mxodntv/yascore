// Package musicyoutube is the YouTube Music column skill.
// Likes and history live in the core music store; this skill only returns intents.
package musicyoutube

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"yascore/config"
	"yascore/internal/domain"
	"yascore/internal/music"
	"yascore/internal/skillstate"
	"yascore/skills/protocol"
)

// Register connects this skill after the core server is already listening.
// store is the shared core music database; it must not be nil.
func Register(ctx context.Context, url string, musicCfg config.MusicYouTube, store *music.Store) error {
	if store == nil {
		return fmt.Errorf("musicyoutube: music store is nil")
	}
	UseService(musicCfg.ServiceURL)
	if err := skillstate.MigrateShared("."); err != nil {
		return err
	}
	speaker := &Speaker{music: store}
	return protocol.RunMedia(ctx, url, "musicyoutube", tools(),
		[]string{"ютуб", "youtube", "музык", "песн", "трек", "плейлист", "альбом", "включи", "поставь", "пауза", "стоп", "следующ", "предыдущ", "назад", "дальше", "вперед", "вперёд", "продолж", "радио", "лайк", "нравится", "похож"},
		speaker.handle, speaker.level)
}

func tools() []domain.Tool {
	return []domain.Tool{
		{
			Name: "play_music",
			Description: "Включить YouTube Music. " +
				"kind=mix — новая подборка. kind=any — продолжить с паузы, иначе случайный лайк или подборка. " +
				"kind=resume — только продолжение с места паузы. " +
				"query — только название. kind=track, playlist, artist. " +
				"kind=likes — лайки по порядку (название и исполнитель). kind=shuffle — лайки вперемешку. " +
				"kind=similar — поиск по исполнителю или названию из лайков.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Название трека, плейлиста или исполнителя. Пусто, если просят просто музыку, лайки или что-нибудь такое же."},
					"kind":  map[string]any{"type": "string", "enum": []any{"any", "mix", "resume", "track", "playlist", "artist", "album", "radio", "likes", "shuffle", "similar"}},
				},
				"required": []any{"kind"},
			},
		},
		{
			Name: "music_control",
			Description: "Управление плеером: pause, resume, next, previous, stop, status, like, unlike. " +
				"previous/next по сохранённой очереди music_plays (сдвиг current); на краю — команды колонки. " +
				"Каждый старт пишет music_history. like — лайк текущего или только что сыгравшего трека. unlike — убрать этот лайк.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":  map[string]any{"type": "string", "enum": []any{"pause", "resume", "next", "previous", "stop", "status", "like", "unlike", "advance"}},
					"title":   map[string]any{"type": "string"},
					"artists": map[string]any{"type": "string"},
					"id":      map[string]any{"type": "string"},
				},
				"required": []any{"action"},
			},
		},
	}
}

// Speaker keeps queue memory. Likes live in the core store.
type Speaker struct {
	music *music.Store

	shuffle []savedTrack
}

func (s *Speaker) level(_ context.Context, arguments string) (string, error) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		body, _ := json.Marshal(domain.SoundLevel{Active: true})
		return string(body), nil
	}
	if _, err := strconv.Atoi(arguments); err != nil {
		return "", fmt.Errorf("громкость вне диапазона")
	}
	return "ok", nil
}

func (s *Speaker) handle(ctx context.Context, name, arguments string) (string, error) {
	switch name {
	case "pause", "resume":
		return "ok", nil
	case "play_music":
		var args struct {
			Query string `json:"query"`
			Kind  string `json:"kind"`
		}
		_ = json.Unmarshal([]byte(arguments), &args)
		return s.playIntent(ctx, args.Query, args.Kind)
	case "music_control":
		var args struct {
			Action  string `json:"action"`
			Title   string `json:"title"`
			Artists string `json:"artists"`
			ID      string `json:"id"`
		}
		_ = json.Unmarshal([]byte(arguments), &args)
		switch strings.ToLower(strings.TrimSpace(args.Action)) {
		case "like":
			return s.likeIntent(ctx, args.Title, args.Artists, args.ID)
		case "unlike":
			return s.unlikeIntent(ctx, args.Title, args.Artists, args.ID)
		case "advance":
			return s.advanceIntent(ctx)
		case "previous", "prev":
			return s.previousIntent(ctx)
		case "next":
			return s.nextIntent(ctx)
		default:
			return intentJSON(domain.MusicIntent{Do: domain.MusicControl, Action: args.Action})
		}
	default:
		return "", fmt.Errorf("unknown tool %s", name)
	}
}

func (s *Speaker) playIntent(ctx context.Context, query, kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "likes":
		return s.playLikesIntent(ctx, false)
	case "shuffle":
		return s.playLikesIntent(ctx, true)
	case "similar":
		return s.playSimilarIntent(ctx)
	case "mix":
		return intentJSON(domain.MusicIntent{Do: domain.MusicPlayMix})
	case "resume":
		return intentJSON(domain.MusicIntent{Do: domain.MusicResumeOnly})
	case "any":
		if vagueMusic(query, kind) {
			return intentJSON(domain.MusicIntent{Do: domain.MusicResumeMix})
		}
	}
	if vagueMusic(query, kind) {
		return intentJSON(domain.MusicIntent{Do: domain.MusicResumeMix})
	}
	return s.namedPlayIntent(ctx, query, kind)
}

func (s *Speaker) namedPlayIntent(_ context.Context, query, kind string) (string, error) {
	query = cleanQuery(query)
	switch kind {
	case "playlist":
		if query == "" {
			return "", fmt.Errorf("не поняла, какой плейлист включить")
		}
		return intentJSON(domain.MusicIntent{Do: domain.MusicPlay, Phrase: playlistPhrase(query)})
	case "album", "artist", "radio":
		if query == "" {
			return "", fmt.Errorf("не поняла, что включить")
		}
		return intentJSON(domain.MusicIntent{Do: domain.MusicPlay, Phrase: query})
	case "likes", "similar":
		return "", fmt.Errorf("лайки берутся из сохранённого списка")
	default:
		if query == "" {
			return "", fmt.Errorf("не поняла, какую песню включить")
		}
		return intentJSON(domain.MusicIntent{Do: domain.MusicSearchPlay, Query: query})
	}
}

func intentJSON(intent domain.MusicIntent) (string, error) {
	body, err := json.Marshal(intent)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func sayIntent(text string) (string, error) {
	return intentJSON(domain.MusicIntent{Do: domain.MusicSay, Text: text})
}

func (s *Speaker) setShuffle(order []savedTrack) {
	if s == nil {
		return
	}
	s.shuffle = append([]savedTrack(nil), order...)
}

func (s *Speaker) clearShuffle() {
	if s == nil {
		return
	}
	s.shuffle = nil
}
