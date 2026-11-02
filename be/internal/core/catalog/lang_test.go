package catalog_test

import (
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestNormalizeLangs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", nil, ""},
		{"already 639-1", []string{"fa"}, "fa"},
		{"639-2/B", []string{"per"}, "fa"},
		{"639-2/T", []string{"fas"}, "fa"},
		// The whole reason this function exists: production stores Persian three ways, and a
		// filter that misses one of them silently hides one file in seven.
		{"three spellings collapse", []string{"per", "fas", "fa"}, "fa"},
		{"audience first", []string{"spa", "eng", "per"}, "fa,en,es"},
		{"rest alphabetical", []string{"tur", "ara", "jpn"}, "ar,ja,tr"},
		{"case and space", []string{" PER ", "Eng"}, "fa,en"},
		{"placeholders dropped", []string{"und", "mul", "", "eng"}, "en"},
		{"no 639-1 dropped", []string{"skr", "eng"}, "en"},
		{"junk dropped", []string{"x", "toolong", "12", "eng"}, "en"},
		{"french both ways", []string{"fre", "fra"}, "fr"},
		{"chinese three ways", []string{"chi", "zho", "cmn"}, "zh"},
		// slo is Slovak in 639-2/B and slv is Slovenian: adjacent codes, different languages.
		{"slovak is not slovenian", []string{"slo", "slv"}, "sk,sl"},
		{"nothing survives", []string{"und", "mul"}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := catalog.NormalizeLangs(tc.in); got != tc.want {
				t.Fatalf("NormalizeLangs(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHasLang(t *testing.T) {
	t.Parallel()

	stored := catalog.NormalizeLangs([]string{"per", "eng", "spa"})
	if stored != "fa,en,es" {
		t.Fatalf("setup: got %q", stored)
	}

	for _, code := range []string{"fa", "en", "es"} {
		if !catalog.HasLang(stored, code) {
			t.Errorf("HasLang(%q, %q) = false", stored, code)
		}
	}
	// A prefix of an entry is not an entry: "e" must not match "en".
	for _, code := range []string{"e", "f", "de", ""} {
		if catalog.HasLang(stored, code) {
			t.Errorf("HasLang(%q, %q) = true", stored, code)
		}
	}
	if catalog.HasLang("", "fa") {
		t.Error("HasLang on empty list returned true")
	}
}

func TestSplitLangs(t *testing.T) {
	t.Parallel()

	if got := catalog.SplitLangs(""); got != nil {
		t.Fatalf("SplitLangs(\"\") = %v, want nil", got)
	}
	got := catalog.SplitLangs("fa,en")
	if len(got) != 2 || got[0] != "fa" || got[1] != "en" {
		t.Fatalf("SplitLangs = %v", got)
	}
}
