package subplus

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// maxSubBytes caps a single extracted subtitle file. Real .srt/.ass top out well under this.
const maxSubBytes = 4 << 20

// SubFile is one subtitle extracted from a pack ZIP, decoded to UTF-8 text with the sub-plus
// advertisement cue removed (doc 15 §6).
type SubFile struct {
	Name string // entry name inside the ZIP, e.g. "News_of_the_World_(2020)_BluRay_-_UTF-8.srt"
	Ext  string // ".srt" or ".ass", lower-case
	Text string // UTF-8, ad cue stripped for .srt
}

// ExtractSubs reads a pack ZIP and returns its .srt/.ass files. Non-subtitle entries and
// unreadable entries are skipped. A ZIP with no usable subtitle is not an error — it returns
// (nil, nil), matching how the app treats "ZIP has no supported subtitle files".
func ExtractSubs(zipBytes []byte) ([]SubFile, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("subplus zip: %w", err)
	}
	var out []SubFile
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(path.Ext(f.Name))
		if ext != ".srt" && ext != ".ass" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxSubBytes))
		rc.Close()
		if err != nil || len(data) == 0 {
			continue
		}
		text := decodeText(data)
		if ext == ".srt" {
			text = stripAdCue(text)
		}
		out = append(out, SubFile{Name: f.Name, Ext: ext, Text: text})
	}
	return out, nil
}

// decodeText turns a subtitle file into UTF-8. Order matters: UTF-16 LE (SubDL's usual
// dump, BOM FF FE) is not valid UTF-8, and treating those bytes as Windows-1256 produces
// `ے‏1` plus NULs — mpv then finds no `-->` cues. Recover that mojibake too, because
// already-cached Telegram copies were uploaded after the old 1256 path.
func decodeText(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		return decodeUTF16(b, unicode.LittleEndian)
	}
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		return decodeUTF16(b, unicode.BigEndian)
	}
	if looksLikeUTF16LE(b) {
		return decodeUTF16(b, unicode.LittleEndian)
	}
	if utf8.Valid(b) {
		s := string(b)
		if rec := recoverUTF16Mojibake(s); rec != "" {
			return rec
		}
		return s
	}
	if dec, err := charmap.Windows1256.NewDecoder().Bytes(b); err == nil {
		if rec := recoverUTF16Mojibake(string(dec)); rec != "" {
			return rec
		}
		return string(dec)
	}
	return string(b)
}

func decodeUTF16(b []byte, endian unicode.Endianness) string {
	dec := unicode.UTF16(endian, unicode.IgnoreBOM).NewDecoder()
	out, err := io.ReadAll(transform.NewReader(bytes.NewReader(b), dec))
	if err != nil || len(out) == 0 {
		return string(b)
	}
	return string(out)
}

func looksLikeUTF16LE(b []byte) bool {
	if len(b) < 8 || len(b)%2 != 0 {
		return false
	}
	nuls := 0
	for i := 1; i < len(b); i += 2 {
		if b[i] == 0 {
			nuls++
		}
	}
	return nuls*4 >= len(b)
}

func recoverUTF16Mojibake(s string) string {
	if !strings.ContainsRune(s, 0) {
		return ""
	}
	if strings.Contains(s, "-->") {
		return ""
	}
	enc, err := charmap.Windows1256.NewEncoder().Bytes([]byte(s))
	if err != nil || len(enc) < 4 {
		return ""
	}
	var text string
	switch {
	case bytes.HasPrefix(enc, []byte{0xFF, 0xFE}):
		text = decodeUTF16(enc, unicode.LittleEndian)
	case bytes.HasPrefix(enc, []byte{0xFE, 0xFF}):
		text = decodeUTF16(enc, unicode.BigEndian)
	case looksLikeUTF16LE(enc):
		text = decodeUTF16(enc, unicode.LittleEndian)
	default:
		return ""
	}
	if strings.Contains(text, "-->") {
		return text
	}
	return ""
}

// stripAdCue drops a leading SRT cue that is a sub-plus advertisement. sub-plus prepends one cue
// (index 0, ~00:00:01 -> 00:00:10) reading "ساب پلاس تقدیم می‌کند …" to every .srt. Only the
// first block is inspected, and only removed if it clearly carries the ad markers; indices are
// left as-is (the SRT spec does not require them to be sequential and players tolerate the gap).
func stripAdCue(srt string) string {
	norm := strings.ReplaceAll(srt, "\r\n", "\n")
	blocks := strings.SplitN(norm, "\n\n", 2)
	if len(blocks) != 2 {
		return srt
	}
	if !looksLikeAd(blocks[0]) {
		return srt
	}
	return strings.TrimLeft(blocks[1], "\n")
}

func looksLikeAd(block string) bool {
	low := strings.ToLower(block)
	if strings.Contains(low, "sub-plus") || strings.Contains(low, "subplus") ||
		strings.Contains(low, "sub plus") || strings.Contains(block, "ساب پلاس") ||
		strings.Contains(block, "ساب‌پلاس") {
		return true
	}
	return false
}
