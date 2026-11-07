package domain

// Fixed spoken replies. Speak caches these as WAV under data/tts-cache.
// Freeform LLM answers are not listed and must not be stored.
const (
	LikedReply   = "Поставила лайк"
	UnlikedReply = "Убрала лайк"
	StoppedReply = "Остановилась."

	NoLikesYet    = "Пока ничего не нравится."
	NothingToLike = "Сейчас нечего лайкать."
	NotInLikes    = "Этого трека нет в лайках."

	AskWeatherPlace = "В какой стране и в каком городе смотреть погоду?"
	AskCurrency     = "Какую валюту запомнить?"
)

// FixedPhrases is every short reply that is always the same text.
// Mood confirmations and current-mood lines are the closed catalog, not model output.
var FixedPhrases = map[string]struct{}{
	LikedReply:   {},
	UnlikedReply: {},
	StoppedReply: {},

	NoLikesYet:    {},
	NothingToLike: {},
	NotInLikes:    {},

	AskWeatherPlace: {},
	AskCurrency:     {},

	"Не удалось узнать погоду.":        {},
	"Не нашла такой город.":            {},
	"Источник погоды не настроен.":     {},
	"Не удалось запомнить город.":      {},
	"Не удалось узнать курс.":          {},
	"Не знаю такую валюту.":            {},
	"Рубль и есть базовая валюта.":     {},
	"Не удалось запомнить валюту.":     {},
	"Не удалось запомнить настроение.": {},
	"Сейчас я как обычно.":             {},
	"Такого настроения нет.":           {},
	"Такого настроения нет. Могу быть доброжелательной, злой, милой, весёлой или спокойной.": {},

	"Хорошо, буду доброжелательной.": {},
	"Хорошо, буду злой.":             {},
	"Хорошо, буду милой.":            {},
	"Хорошо, буду весёлой.":          {},
	"Хорошо, буду спокойной.":        {},
	"Сейчас я доброжелательная.":     {},
	"Сейчас я злая.":                 {},
	"Сейчас я милая.":                {},
	"Сейчас я весёлая.":              {},
	"Сейчас я спокойная.":            {},
}

// IsFixedPhrase reports a reply that may be served from the TTS WAV cache.
func IsFixedPhrase(text string) bool {
	_, ok := FixedPhrases[text]
	return ok
}
