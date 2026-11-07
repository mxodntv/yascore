package currency

import (
	"strings"
	"unicode"
)

func speak(form nameForm, nominal int, rubles float64) string {
	n := int(roundHalfAway(rubles))
	if n < 0 {
		n = -n
	}
	money := cardinal(n) + " " + rubleWord(n)
	if nominal <= 1 {
		return form.one + " стоит " + money
	}
	unit := form.units
	if unit == "" {
		unit = strings.ToLower(form.one)
	}
	return capitalize(cardinal(nominal)) + " " + unit + " стоят " + money
}

func roundHalfAway(v float64) float64 {
	if v < 0 {
		return -roundHalfAway(-v)
	}
	return float64(int(v + 0.5))
}

func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func rubleWord(n int) string {
	if n < 0 {
		n = -n
	}
	n %= 100
	if n >= 11 && n <= 14 {
		return "рублей"
	}
	switch n % 10 {
	case 1:
		return "рубль"
	case 2, 3, 4:
		return "рубля"
	default:
		return "рублей"
	}
}

func cardinal(n int) string {
	if n < 0 {
		n = -n
	}
	if n < 1000 {
		return under1000(n)
	}
	if n < 1000000 {
		word := thousands(n / 1000)
		if r := n % 1000; r != 0 {
			word += " " + under1000(r)
		}
		return word
	}
	word := cardinal(n / 1000000)
	word += " " + plural(n/1000000, "миллион", "миллиона", "миллионов")
	if r := n % 1000000; r != 0 {
		word += " " + cardinal(r)
	}
	return word
}

func thousands(n int) string {
	if n == 1 {
		return "одна тысяча"
	}
	return feminine(n) + " " + plural(n, "тысяча", "тысячи", "тысяч")
}

func feminine(n int) string {
	word := under1000(n)
	if n%100 == 11 || n%100 == 12 {
		return word
	}
	switch n % 10 {
	case 1:
		return strings.TrimSuffix(word, "один") + "одна"
	case 2:
		return strings.TrimSuffix(word, "два") + "две"
	default:
		return word
	}
}

func plural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	n %= 100
	if n >= 11 && n <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

func under1000(n int) string {
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
	word := ruHundreds[n/100]
	if r := n % 100; r != 0 {
		word += " " + under1000(r)
	}
	return word
}

var (
	ruOnes     = []string{"ноль", "один", "два", "три", "четыре", "пять", "шесть", "семь", "восемь", "девять"}
	ruTeens    = []string{"десять", "одиннадцать", "двенадцать", "тринадцать", "четырнадцать", "пятнадцать", "шестнадцать", "семнадцать", "восемнадцать", "девятнадцать"}
	ruTens     = []string{"", "", "двадцать", "тридцать", "сорок", "пятьдесят", "шестьдесят", "семьдесят", "восемьдесят", "девяносто"}
	ruHundreds = []string{"", "сто", "двести", "триста", "четыреста", "пятьсот", "шестьсот", "семьсот", "восемьсот", "девятьсот"}
)
