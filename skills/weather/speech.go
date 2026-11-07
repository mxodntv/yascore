package weather

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

func speak(city string, obs Observation) string {
	return fmt.Sprintf("В городе %s сейчас %s и %s", cityName(city), tempWords(obs.TemperatureC), condition(obs.WeatherCode, obs.Precipitation))
}

func cityName(city string) string {
	city = strings.TrimSpace(city)
	if city == "" {
		return "этом городе"
	}
	r := []rune(city)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func tempWords(celsius float64) string {
	n := int(math.Round(celsius))
	if n < 0 {
		return "минус " + cardinal(-n)
	}
	return cardinal(n)
}

func cardinal(n int) string {
	if n < 0 {
		n = -n
	}
	if n < 10 {
		return ruOnes[n]
	}
	if n < 20 {
		return ruTeens[n-10]
	}
	if n < 100 {
		word := ruTens[n/10]
		if r := n % 10; r != 0 {
			word += " " + ruOnes[r]
		}
		return word
	}
	if n < 1000 {
		word := ruHundreds[n/100]
		if r := n % 100; r != 0 {
			word += " " + cardinal(r)
		}
		return word
	}
	word := cardinal(n / 1000)
	if r := n % 1000; r != 0 {
		word += " " + cardinal(r)
	}
	return word
}

var (
	ruOnes     = []string{"ноль", "один", "два", "три", "четыре", "пять", "шесть", "семь", "восемь", "девять"}
	ruTeens    = []string{"десять", "одиннадцать", "двенадцать", "тринадцать", "четырнадцать", "пятнадцать", "шестнадцать", "семнадцать", "восемнадцать", "девятнадцать"}
	ruTens     = []string{"", "", "двадцать", "тридцать", "сорок", "пятьдесят", "шестьдесят", "семьдесят", "восемьдесят", "девяносто"}
	ruHundreds = []string{"", "сто", "двести", "триста", "четыреста", "пятьсот", "шестьсот", "семьсот", "восемьсот", "девятьсот"}
)

func condition(code int, precip float64) string {
	if wet(code, precip) {
		return wetPhrase(code)
	}
	switch code {
	case 0:
		return "ясно"
	case 1:
		return "малооблачно"
	case 2:
		return "облачно"
	case 3:
		return "пасмурно"
	case 45, 48:
		return "туман"
	default:
		return "без осадков"
	}
}

func wet(code int, precip float64) bool {
	if precip >= 0.1 {
		return true
	}
	switch code {
	case 51, 53, 55, 56, 57, 61, 63, 65, 66, 67, 71, 73, 75, 77, 80, 81, 82, 85, 86, 95, 96, 99:
		return true
	default:
		return false
	}
}

func wetPhrase(code int) string {
	switch code {
	case 71, 73, 75, 77, 85, 86:
		return "идёт снег"
	case 95, 96, 99:
		return "гроза"
	case 56, 57, 66, 67:
		return "ледяной дождь"
	default:
		return "идёт дождь"
	}
}
