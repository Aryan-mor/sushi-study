package newsroom

import "testing"

func TestCountryLine(t *testing.T) {
	t.Parallel()
	if got := countryLine("US"); got != "🇺🇸 آمریکا" {
		t.Fatalf("US = %q", got)
	}
	if got := countryLine("uk"); got != "🇬🇧 انگلستان" {
		t.Fatalf("UK = %q", got)
	}
	if countryLine("") != "" || countryLine("-") != "" {
		t.Fatal("empty / CountryNone must omit")
	}
	if got := countryLine("ZZ"); got != "🇿🇿" {
		t.Fatalf("unknown ISO should be flag only, got %q", got)
	}
}
