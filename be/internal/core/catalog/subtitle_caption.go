package catalog

import (
	"fmt"
	"strings"
)

// SubtitleCaption is the caption indexerd stamps on a subtitle-channel row's storage_caption
// (ingest_queue), for the reader to send verbatim when it archives the extracted file into
// SUBTITLE_CHANNEL_ID -- the same "write it once in Go, the reader just sends it" split
// FormatStorageCaption already uses for the video clone channel.
type SubtitleCaption struct {
	Title           string
	Year            int32
	Kind            int16 // 1 movie, 2 series
	SeasonNo        int32
	EpisodeNo       int32
	Lang            string // ISO 639-1
	FileName        string
	TmdbID          int64
	SourceChannelID int64
	SourceMsgID     int64
}

// FormatSubtitleCaption renders the caption for an archived subtitle file: what it is (title,
// season/episode, language), the original file name, and a link back to the source post -- the
// subtitle-channel counterpart of FormatStorageCaption, so scrolling SUBTITLE_CHANNEL_ID reads the
// same way scrolling the video clone channel does.
func FormatSubtitleCaption(c SubtitleCaption) string {
	head := subtitleHumanBlock(c)
	coords, coordsPlain := subtitleCoordsLine(c)

	budget := storageCaptionMax - len(coordsPlain) - 1
	if budget < 32 {
		budget = 32
	}
	return esc(clampRunes(head, budget)) + "\n" + coords
}

func subtitleHumanBlock(c SubtitleCaption) string {
	title := strings.TrimSpace(c.Title)
	if title == "" {
		title = "untitled"
	}

	var b strings.Builder
	b.WriteString("💬 ")
	b.WriteString(title)
	if c.Year > 0 {
		fmt.Fprintf(&b, " (%d)", c.Year)
	}

	if c.Kind == 2 && (c.SeasonNo > 0 || c.EpisodeNo > 0) {
		fmt.Fprintf(&b, "\nS%02dE%02d", c.SeasonNo, c.EpisodeNo)
	}
	if lang := subtitleLangLabel(c.Lang); lang != "" {
		b.WriteString("\n🗣 ")
		b.WriteString(lang)
	}
	if name := oneLine(c.FileName); name != "" {
		b.WriteString("\n📄 ")
		b.WriteString(name)
	}
	return b.String()
}

// subtitleLangLabel renders a human label for the languages sushi indexes subtitles for; an
// unrecognised ISO 639-1 code still shows, upper-cased, rather than being dropped silently.
func subtitleLangLabel(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "":
		return ""
	case "fa":
		return "فارسی"
	default:
		return strings.ToUpper(code)
	}
}

func subtitleCoordsLine(c SubtitleCaption) (string, string) {
	branch := "movie"
	if c.Kind == 2 {
		branch = "tv"
	}

	var h, p strings.Builder
	h.WriteString("🔗 ")
	p.WriteString("🔗 ")

	if c.TmdbID > 0 {
		fmt.Fprintf(&h, `<a href="https://www.themoviedb.org/%s/%d">tmdb</a> <code>%d</code>`, branch, c.TmdbID, c.TmdbID)
		fmt.Fprintf(&p, "tmdb %d", c.TmdbID)
	} else {
		h.WriteString(branch)
		p.WriteString(branch)
	}

	if link := sourceLink(c.SourceChannelID, c.SourceMsgID); link != "" {
		fmt.Fprintf(&h, ` · <a href="%s">Source</a>`, link)
		p.WriteString(" · Source")
	}

	return h.String(), p.String()
}
