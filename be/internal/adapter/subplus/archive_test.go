package subplus

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

const (
	adCue   = "0\n00:00:01,000 --> 00:00:10,000\n<font>ساب پلاس تقدیم می‌کند</font>\n"
	realCue = "1\n00:01:00,000 --> 00:01:02,000\nHello there.\n"
)

func buildZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractSubs(t *testing.T) {
	t.Parallel()
	// Arabic-form letters only: Windows-1256 has no Farsi yeh (U+06CC) or keheh (U+06A9), which
	// is exactly why real "ANSI" Persian subs render as mojibake without this decode path.
	persian := "سلام جهان"
	ansi, err := charmap.Windows1256.NewEncoder().Bytes([]byte("2\n00:02:00,000 --> 00:02:02,000\n" + persian + "\n"))
	if err != nil {
		t.Fatalf("cp1256 encode: %v", err)
	}

	zbytes := buildZip(t, map[string][]byte{
		"movie_-_UTF-8.srt": []byte(adCue + "\n" + realCue),
		"movie_-_ANSI.srt":  ansi,
		"styled.ass":        []byte("[Script Info]\n; " + persian + "\n"),
		"readme.txt":        []byte("ignore me"),
	})

	subs, err := ExtractSubs(zbytes)
	if err != nil {
		t.Fatalf("ExtractSubs: %v", err)
	}
	if len(subs) != 3 {
		t.Fatalf("got %d subs, want 3: %+v", len(subs), names(subs))
	}

	byName := map[string]SubFile{}
	for _, s := range subs {
		byName[s.Name] = s
	}

	utf8Sub := byName["movie_-_UTF-8.srt"]
	if strings.Contains(utf8Sub.Text, "ساب پلاس") {
		t.Errorf("ad cue not stripped: %q", utf8Sub.Text)
	}
	if !strings.Contains(utf8Sub.Text, "Hello there.") || !strings.HasPrefix(utf8Sub.Text, "1\n") {
		t.Errorf("real cue missing or offset: %q", utf8Sub.Text)
	}

	ansiSub := byName["movie_-_ANSI.srt"]
	if !utf8.ValidString(ansiSub.Text) || !strings.Contains(ansiSub.Text, persian) {
		t.Errorf("cp1256 not decoded to utf-8: %q", ansiSub.Text)
	}

	assSub := byName["styled.ass"]
	if assSub.Ext != ".ass" || !strings.Contains(assSub.Text, "[Script Info]") {
		t.Errorf(".ass mangled: %+v", assSub)
	}
}

func TestExtractSubsNoSubtitles(t *testing.T) {
	t.Parallel()
	subs, err := ExtractSubs(buildZip(t, map[string][]byte{"a.txt": []byte("x"), "b.nfo": []byte("y")}))
	if err != nil || subs != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", subs, err)
	}
}

func TestExtractSubsBadZip(t *testing.T) {
	t.Parallel()
	if _, err := ExtractSubs([]byte("not a zip")); err == nil {
		t.Fatal("want error for non-zip bytes")
	}
}

func TestStripAdCueLeavesCleanSrt(t *testing.T) {
	t.Parallel()
	clean := realCue + "\n2\n00:03:00,000 --> 00:03:02,000\nGeneral Kenobi.\n"
	if got := stripAdCue(clean); got != clean {
		t.Errorf("clean srt changed:\n%q\n->\n%q", clean, got)
	}
}

func TestDecodeTextTrimsBOM(t *testing.T) {
	t.Parallel()
	if got := decodeText([]byte{0xEF, 0xBB, 0xBF, 'h', 'i'}); got != "hi" {
		t.Errorf("BOM not trimmed: %q", got)
	}
}

func TestDecodeTextUTF16LE(t *testing.T) {
	t.Parallel()
	const srt = "1\n00:00:01,000 --> 00:00:02,000\nسلام\n"
	enc := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder()
	raw, err := enc.Bytes([]byte(srt))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeText(raw)
	if !strings.Contains(got, "-->") || !strings.Contains(got, "سلام") {
		t.Fatalf("utf-16 le: %q", got)
	}

	mojibake, err := charmap.Windows1256.NewDecoder().Bytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	got = decodeText(mojibake)
	if !strings.Contains(got, "-->") || !strings.Contains(got, "سلام") {
		t.Fatalf("utf-16 via cp1256 mojibake: %q", got)
	}
}

func names(subs []SubFile) []string {
	out := make([]string, len(subs))
	for i, s := range subs {
		out[i] = s.Name
	}
	return out
}
