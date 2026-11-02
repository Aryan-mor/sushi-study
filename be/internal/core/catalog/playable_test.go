package catalog

import "testing"

func TestFileHiddenForNonPersianSubtitle(t *testing.T) {
	t.Parallel()

	hidden := []struct{ label, subs string }{
		{"480p HDRip Dub", ""},
		{"720p HDRip Dub", ""},
		{"480p HDTS", ""},
		{"720p HDTS", ""},
		{"1080p hardsub", ""},
		{"1080p Dub Persian", "fa"},
	}
	for _, c := range hidden {
		if !FileHiddenForNonPersianSubtitle(c.label, c.subs) {
			t.Errorf("%q subs=%q should be hidden", c.label, c.subs)
		}
	}

	kept := []struct{ label, subs string }{
		{"1080p SoftSub", "en"},
		{"1080p", ""},
		{"1080p WEB-DL", "en,es"},
		{"زیرنویس_فارسی_چسبیده", "fa"},
	}
	for _, c := range kept {
		if FileHiddenForNonPersianSubtitle(c.label, c.subs) {
			t.Errorf("%q subs=%q should stay playable", c.label, c.subs)
		}
	}
}

func TestSubtitleLangIsPersian(t *testing.T) {
	t.Parallel()
	if !SubtitleLangIsPersian("") || !SubtitleLangIsPersian("fa") || !SubtitleLangIsPersian("Persian") {
		t.Fatal("empty and Persian should count as Persian")
	}
	if SubtitleLangIsPersian("en") {
		t.Fatal("en is not Persian")
	}
}
