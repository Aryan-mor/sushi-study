package newsroom

import (
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// countryLine is the prototype formatCountryFA: flag + Persian name. Empty / CountryNone omit the row.
func countryLine(code string) string {
	code = catalog.CountryISO(code)
	if code == "" || code == catalog.CountryNone {
		return ""
	}
	name := countryNameFA(code)
	flag := countryFlagEmoji(code)
	if flag == "" {
		return name
	}
	if strings.EqualFold(name, code) {
		return flag
	}
	return flag + " " + name
}

func countryNameFA(code string) string {
	if name, ok := countryNamesFA[code]; ok {
		return name
	}
	return code
}

func countryFlagEmoji(code string) string {
	if len(code) != 2 {
		return ""
	}
	const base = 0x1F1E6 // regional indicator symbol letter A
	return string([]rune{rune(base + int(code[0]-'A')), rune(base + int(code[1]-'A'))})
}

// countryNamesFA is the prototype's channel_news_card.go map. Unknown ISO codes fall back to the code.
var countryNamesFA = map[string]string{
	"US": "آمریکا",
	"GB": "انگلستان",
	"CA": "کانادا",
	"AU": "استرالیا",
	"DE": "آلمان",
	"FR": "فرانسه",
	"IT": "ایتالیا",
	"ES": "اسپانیا",
	"JP": "ژاپن",
	"KR": "کره جنوبی",
	"CN": "چین",
	"IN": "هند",
	"TR": "ترکیه",
	"BR": "برزیل",
	"IR": "ایران",
	"SE": "سوئد",
	"NO": "نروژ",
	"DK": "دانمارک",
	"NL": "هلند",
	"BE": "بلژیک",
	"CH": "سوئیس",
	"AT": "اتریش",
	"RU": "روسیه",
	"MX": "مکزیک",
	"AR": "آرژانتین",
	"NZ": "نیوزیلند",
	"IE": "ایرلند",
	"PL": "لهستان",
}
