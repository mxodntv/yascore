// Package youtubemusic is the YouTube Music skill.
// Likes and play history are the youtubemusic_ tables in data/yascore.db.
// The skill never opens SSH: it returns intents and the core runs the column.
package youtubemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"yascore/config"
	"yascore/internal/domain"
	"yascore/internal/skillstate"
	"yascore/skills/protocol"
)

// Register connects this skill after the core server is already listening.
func Register(ctx context.Context, url string, musicCfg config.YouTubeMusic) error {
	UseService(musicCfg.ServiceURL)
	if err := skillstate.MigrateShared("."); err != nil {
		return err
	}
	music, err := Open(musicCfg.Database)
	if err != nil {
		return err
	}
	defer music.Close()
	speaker := &Speaker{music: music}
	return protocol.RunMedia(ctx, url, "youtubemusic", tools(),
		[]string{"ютуб", "youtube", "музык", "песн", "трек", "плейлист", "альбом", "включи", "поставь", "пауза", "стоп", "следующ", "предыдущ", "назад", "дальше", "вперед", "вперёд", "продолж", "радио", "лайк", "нравится", "похож"},
		speaker.handle, speaker.level)
}

func tools() []domain.Tool {
	return []domain.Tool{
		{
			Name: "play_music",
			Description: "Включить YouTube Music. " +
				"Если сказали просто «включи музыку», без названия — kind=any и пустой query: продолжится остановленный трек, а если очереди нет, включится подборка. " +
				"query — только название. kind=track, playlist, artist или any. " +
				"kind=likes и пустой query — трек из лайков, сохранённых ассистентом. " +
				"kind=similar и пустой query — поиск по исполнителю или названию из этих лайков.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Название трека, плейлиста или исполнителя. Пусто, если просят просто музыку, лайки или что-нибудь такое же."},
					"kind":  map[string]any{"type": "string", "enum": []any{"any", "track", "playlist", "artist", "album", "radio", "likes", "similar"}},
				},
				"required": []any{"kind"},
			},
		},
		{
			Name: "music_control",
			Description: "Управление плеером: pause, resume, next, previous, stop, status, like, unlike. " +
				"previous включает предыдущий сохранённый трек по названию и оставляет текущий следующим. " +
				"next включает этот следующий трек, если он уже отложен. " +
				"like — лайк текущего или только что сыгравшего трека. unlike — убрать этот лайк.",
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

// Speaker keeps likes and queue memory. The column is driven by the core.
type Speaker struct {
	music *Store
}

// level reports whether the player is making sound. Loudness lives in the core.
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

func (s Speaker) handle(ctx context.Context, name, arguments string) (string, error) {
	switch name {
	case "pause", "resume":
		// Core owns pause/resume on the column. Acknowledge for old bus clients.
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

func (s Speaker) playIntent(ctx context.Context, query, kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "likes":
		return s.playLikesIntent(ctx)
	case "similar":
		return s.playSimilarIntent(ctx)
	}
	if vagueMusic(query, kind) {
		return intentJSON(domain.MusicIntent{Do: domain.MusicResumeMix})
	}
	return s.namedPlayIntent(ctx, query, kind)
}

func (s Speaker) namedPlayIntent(_ context.Context, query, kind string) (string, error) {
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
