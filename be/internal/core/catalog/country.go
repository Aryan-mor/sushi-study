package catalog

import (
	"encoding/json"
	"strings"
)

// PersianOrigin is true for TMDB origin countries whose films and series are Persian-language.
// A Persian audio track there is the original, not a dub. Same set as reader.PERSIAN_COUNTRIES.
func PersianOrigin(code string) bool {
	switch CountryISO(code) {
	case "IR", "AF", "TJ":
		return true
	default:
		return false
	}
}

// CountryISO normalises a TMDB origin / production country to ISO 3166-1 alpha-2.
// UK is stored as GB (the flag regional-indicator pair). Anything else is empty.
func CountryISO(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "UK" {
		code = "GB"
	}
	if len(code) != 2 {
		return ""
	}
	if code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return code
}

// FirstOriginCountry prefers TMDB origin_country, then production_countries.
func FirstOriginCountry(origin, production []string) string {
	for _, c := range origin {
		if code := CountryISO(c); code != "" {
			return code
		}
	}
	for _, c := range production {
		if code := CountryISO(c); code != "" {
			return code
		}
	}
	return ""
}

// OriginCountryJSON reads TMDB-shaped origin_country (string or string array) and
// production_countries ([{iso_3166_1}]).
func OriginCountryJSON(originRaw, productionRaw json.RawMessage) string {
	return FirstOriginCountry(originCountryCodes(originRaw), productionCountryCodes(productionRaw))
}

func originCountryCodes(raw json.RawMessage) []string {
	if codes := jsonStringSlice(raw); len(codes) > 0 {
		return codes
	}
	if s := strings.TrimSpace(jsonString(raw)); s != "" {
		return []string{s}
	}
	return nil
}

func productionCountryCodes(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var rows []struct {
		Iso31661 string `json:"iso_3166_1"`
		Iso      string `json:"iso"`
		Code     string `json:"code"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return jsonStringSlice(raw)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		for _, c := range []string{r.Iso31661, r.Iso, r.Code} {
			if strings.TrimSpace(c) != "" {
				out = append(out, c)
				break
			}
		}
	}
	return out
}
