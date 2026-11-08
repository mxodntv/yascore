package usecase

import (
	"strings"

	"yascore/internal/domain"
)

// phrase is a bare command after wake-word stripping: lower case, no edge punctuation.
func phrase(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.Trim(text, " \t\r\n.,!?;:«»\"'"))), " ")
}

// globalStop pauses every skill and the conversation. It is not a music-only stop.
// Leading wake words are already stripped by the time the line arrives here.
func globalStop(text string) bool {
	switch phrase(text) {
	case "стоп", "хватит", "остановись", "замолчи", "тише", "помолчи":
		return true
	default:
		return false
	}
}

// playerCommand is a bare player phrase. It skips the model.
type playerCommand struct {
	tool     string
	args     string
	memory   string
	keepHold bool // like/unlike: wake pause must resume after the spoken reply
}

// playerAction recognizes a bare player command after the wake names are gone.
// Phrases that name music stay here. Bare «стоп» is globalStop, not this.
func playerAction(text string) (playerCommand, bool) {
	switch phrase(text) {
	case "стоп музыка", "стоп музыку", "останови", "останови музыку", "выключи музыку", "выключи ютуб":
		return controlCommand("stop", "Остановила музыку."), true
	case "пауза", "поставь на паузу":
		return controlCommand("pause", "Пауза."), true
	case "дальше", "вперед", "вперёд", "следующий", "следующий трек", "следующая", "следующая песня":
		return controlCommand("next", "Следующий трек."), true
	case "назад", "предыдущий", "предыдущий трек", "предыдущая песня":
		return controlCommand("previous", "Предыдущий трек."), true
	case "продолжи", "продолжи музыку", "продолжай", "продолжай музыку", "продолжи песню", "продолжай песню":
		return playerCommand{tool: "play_music", args: `{"query":"","kind":"resume"}`, memory: "Продолжила музыку."}, true
	case "включи другое", "включи музыку", "поставь музыку":
		return playerCommand{tool: "play_music", args: `{"query":"","kind":"mix"}`, memory: "Включила музыку."}, true
	case "музыка", "музыку", "включай", "включай музыку", "играй", "играй музыку":
		return playerCommand{tool: "play_music", args: `{"query":"","kind":"any"}`, memory: "Включила музыку."}, true
	case "лайк", "нравится", "like":
		return holdControl("like", domain.LikedReply), true
	case "убери лайк", "не нравится", "unlike":
		return holdControl("unlike", domain.UnlikedReply), true
	case "включи лайки", "включи что-нибудь из лайков", "включи что нибудь из лайков":
		return playerCommand{tool: "play_music", args: `{"query":"","kind":"likes"}`, memory: "Включила из лайков."}, true
	case "включи лайки вперемешку", "лайки вперемешку", "включи любимое вперемешку", "любимое вперемешку":
		return playerCommand{tool: "play_music", args: `{"query":"","kind":"shuffle"}`, memory: "Включила из лайков."}, true
	case "что-нибудь такое же", "что нибудь такое же", "что-нибудь похожее", "что нибудь похожее":
		return playerCommand{tool: "play_music", args: `{"query":"","kind":"similar"}`, memory: "Похожее."}, true
	default:
		return playerCommand{}, false
	}
}

func controlCommand(action, memory string) playerCommand {
	return playerCommand{tool: "music_control", args: `{"action":"` + action + `"}`, memory: memory}
}

func holdControl(action, memory string) playerCommand {
	cmd := controlCommand(action, memory)
	cmd.keepHold = true
	return cmd
}
