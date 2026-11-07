package currency

import "strings"

// nameForm is how a currency is spoken. units is the genitive plural after a nominal above one.
type nameForm struct {
	one   string
	units string
}

const rubPhrase = "Рубль и есть базовая валюта."

func classify(raw string) (code string, form nameForm, kind int) {
	n := normalize(raw)
	switch n {
	case "", "рубль", "рубля", "рубли", "рублей", "rub", "rur":
		if n == "" {
			return "", nameForm{}, kindEmpty
		}
		return "RUB", nameForm{}, kindRUB
	}
	if code, ok := aliases[n]; ok {
		return code, forms[code], kindKnown
	}
	if isCode(n) {
		code = strings.ToUpper(n)
		if code == "RUB" || code == "RUR" {
			return "RUB", nameForm{}, kindRUB
		}
		return code, forms[code], kindCode
	}
	return "", nameForm{}, kindUnknown
}

const (
	kindEmpty = iota
	kindRUB
	kindKnown
	kindCode
	kindUnknown
)

func normalize(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "ё", "е")
	raw = strings.Join(strings.Fields(raw), " ")
	raw = strings.TrimPrefix(raw, "курс ")
	return strings.TrimSpace(raw)
}

func isCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func formOrXML(code string, xmlName string) nameForm {
	if form, ok := forms[code]; ok && form.one != "" {
		return form
	}
	name := strings.TrimSpace(xmlName)
	if name == "" {
		name = code
	}
	return nameForm{one: capitalize(name), units: strings.ToLower(name)}
}

var forms = map[string]nameForm{
	"USD": {"Доллар", "долларов"},
	"EUR": {"Евро", "евро"},
	"CNY": {"Юань", "юаней"},
	"KZT": {"Тенге", "тенге"},
	"GBP": {"Фунт", "фунтов"},
	"JPY": {"Иена", "иен"},
	"CHF": {"Франк", "франков"},
	"TRY": {"Лира", "лир"},
	"BYN": {"Белорусский рубль", "белорусских рублей"},
	"UAH": {"Гривна", "гривен"},
	"PLN": {"Злотый", "злотых"},
	"KGS": {"Сом", "сомов"},
	"AMD": {"Драм", "драмов"},
	"AZN": {"Манат", "манатов"},
	"UZS": {"Сум", "сумов"},
	"INR": {"Рупия", "рупий"},
	"CAD": {"Канадский доллар", "канадских долларов"},
	"AUD": {"Австралийский доллар", "австралийских долларов"},
	"GEL": {"Лари", "лари"},
	"AED": {"Дирхам", "дирхамов"},
	"KRW": {"Вона", "вон"},
	"THB": {"Бат", "батов"},
	"SEK": {"Шведская крона", "шведских крон"},
	"NOK": {"Норвежская крона", "норвежских крон"},
	"DKK": {"Датская крона", "датских крон"},
	"CZK": {"Чешская крона", "чешских крон"},
	"HUF": {"Форинт", "форинтов"},
	"BRL": {"Реал", "реалов"},
}

var aliases = map[string]string{
	"доллар": "USD", "доллара": "USD", "доллару": "USD", "долларом": "USD",
	"доллары": "USD", "долларов": "USD", "бакс": "USD", "баксы": "USD", "бакса": "USD",
	"доллар сша": "USD", "американский доллар": "USD", "usd": "USD",
	"евро": "EUR", "eur": "EUR",
	"юань": "CNY", "юаня": "CNY", "юаню": "CNY", "юанем": "CNY", "юани": "CNY", "юаней": "CNY",
	"китайский юань": "CNY", "юань китайский": "CNY", "cny": "CNY", "rmb": "CNY",
	"тенге": "KZT", "казахстанский тенге": "KZT", "казахский тенге": "KZT", "kzt": "KZT",
	"фунт": "GBP", "фунта": "GBP", "фунту": "GBP", "фунтов": "GBP", "фунты": "GBP",
	"фунт стерлингов": "GBP", "gbp": "GBP",
	"иена": "JPY", "иены": "JPY", "иену": "JPY", "иен": "JPY", "йена": "JPY", "йены": "JPY", "jpy": "JPY",
	"франк": "CHF", "франка": "CHF", "франков": "CHF", "швейцарский франк": "CHF", "chf": "CHF",
	"лира": "TRY", "лиры": "TRY", "лир": "TRY", "турецкая лира": "TRY", "try": "TRY",
	"гривна": "UAH", "гривны": "UAH", "гривен": "UAH", "uah": "UAH",
	"белорусский рубль": "BYN", "byn": "BYN",
	"злотый": "PLN", "злотых": "PLN", "pln": "PLN",
	"сом": "KGS", "сома": "KGS", "сомов": "KGS", "kgs": "KGS",
	"драм": "AMD", "драма": "AMD", "драмов": "AMD", "amd": "AMD",
	"манат": "AZN", "маната": "AZN", "манатов": "AZN", "azn": "AZN",
	"сум": "UZS", "сума": "UZS", "сумов": "UZS", "uzs": "UZS",
	"рупия": "INR", "рупии": "INR", "рупий": "INR", "inr": "INR",
	"лари": "GEL", "gel": "GEL",
	"дирхам": "AED", "дирхама": "AED", "дирхамов": "AED", "aed": "AED",
	"вона": "KRW", "воны": "KRW", "вон": "KRW", "krw": "KRW",
	"форинт": "HUF", "форинта": "HUF", "форинтов": "HUF", "huf": "HUF",
	"реал": "BRL", "реала": "BRL", "реалов": "BRL", "brl": "BRL",
	"бат": "THB", "бата": "THB", "батов": "THB", "thb": "THB",
	"канадский доллар": "CAD", "cad": "CAD",
	"австралийский доллар": "AUD", "aud": "AUD",
}
