package catalog

import "testing"

func TestPersianOrigin(t *testing.T) {
	t.Parallel()
	if !PersianOrigin("ir") || !PersianOrigin("AF") || !PersianOrigin("tj") {
		t.Fatal("IR/AF/TJ are Persian-language origins")
	}
	if PersianOrigin("US") || PersianOrigin("") || PersianOrigin("-") {
		t.Fatal("other codes are not Persian-language origins")
	}
}

func TestCountryISO(t *testing.T) {
	t.Parallel()
	if got := CountryISO("us"); got != "US" {
		t.Fatalf("US: %q", got)
	}
	if got := CountryISO("uk"); got != "GB" {
		t.Fatalf("UK: %q", got)
	}
	if CountryISO("") != "" || CountryISO("USA") != "" || CountryISO("-") != "" {
		t.Fatal("invalid codes must be empty")
	}
}

func TestFirstOriginCountryPrefersOrigin(t *testing.T) {
	t.Parallel()
	got := FirstOriginCountry([]string{"US"}, []string{"GB"})
	if got != "US" {
		t.Fatalf("got %q, want US", got)
	}
	got = FirstOriginCountry(nil, []string{" jp "})
	if got != "JP" {
		t.Fatalf("production fallback %q", got)
	}
}

func TestOriginCountryJSON(t *testing.T) {
	t.Parallel()
	got := OriginCountryJSON([]byte(`["US"]`), []byte(`[{"iso_3166_1":"GB"}]`))
	if got != "US" {
		t.Fatalf("array origin %q", got)
	}
	got = OriginCountryJSON([]byte(`"ir"`), nil)
	if got != "IR" {
		t.Fatalf("string origin %q", got)
	}
	got = OriginCountryJSON(nil, []byte(`[{"iso_3166_1":"FR"}]`))
	if got != "FR" {
		t.Fatalf("production %q", got)
	}
}
