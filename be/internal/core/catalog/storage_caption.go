package catalog

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const storageCaptionMax = 1024

// StorageCaption is the clone/backup channel caption. Delivery to a reader uses a different
// caption (the locator in docs/05). This one is for us: scrolling the channel, and rebuilding
// coordinates if the library database is gone.
type StorageCaption struct {
	FileID       int64
	TmdbID       int64
	Title        string
	Year         int32
	Kind         int16
	SeasonNo     int32
	EpisodeNo    int32
	EpisodeTitle string
	FileName     string
	DurationS    int32
	AudioLangs   string
	SubLangs     string
	Quality      string
	Source       string
	ExtKey       string
	// SourceChannelID/SourceMsgID locate the exact post this file was indexed from (Bot-API form
	// channel id, e.g. -1009900000005) -- fetcherd's the prototype channel or indexerd's tgforward
	// channel, whichever path found the file. Zero on a caption already written before this field
	// existed (old files, never rewritten), so those keep rendering with no Source link.
	SourceChannelID int64
	SourceMsgID     int64
}

// FormatStorageCaption renders the caption stamped on the clone copy. The backup channels inherit
// it verbatim -- Telegram carries a caption and its entities forward on a copyMessage that gives
// no new caption -- so this is the one place the format is decided.
//
// parse_mode=HTML (set by both senders: tg.CopyMessage and the reader act pass). Only the last
// line carries markup: the word "tmdb" links to the title on themoviedb.org and every id sits in
// <code> so a tap copies it. Everything above is one fact per line behind an icon, plain text,
// HTML-escaped because a title or file name may hold < > &.
func FormatStorageCaption(c StorageCaption) string {
	head := humanBlock(c)
	coords, coordsPlain := coordsLine(c)

	// Budget the human block against the plain-text width of the coords line: Telegram measures
	// the 1024 limit on the text after entities are parsed, not on the markup we send.
	budget := storageCaptionMax - len(coordsPlain) - 1
	if budget < 32 {
		budget = 32
	}
	// Clamp first, escape second: a cut can land mid-word but never mid-entity.
	return esc(clampRunes(head, budget)) + "\n" + coords
}

// humanBlock is the readable part -- one concern per line, each line led by an icon so it says
// what it is without a key=value legend. Plain text; FormatStorageCaption escapes it as one blob.
func humanBlock(c StorageCaption) string {
	title := strings.TrimSpace(c.Title)
	if title == "" {
		title = "untitled"
	}

	var b strings.Builder
	if c.Kind == 2 {
		b.WriteString("📺 ")
	} else {
		b.WriteString("🎬 ")
	}
	b.WriteString(title)
	if c.Year > 0 {
		fmt.Fprintf(&b, " (%d)", c.Year)
	}

	if c.Kind == 2 && (c.SeasonNo > 0 || c.EpisodeNo > 0) {
		fmt.Fprintf(&b, "\nS%02dE%02d", c.SeasonNo, c.EpisodeNo)
		if ep := strings.TrimSpace(c.EpisodeTitle); ep != "" {
			b.WriteString(" · ")
			b.WriteString(ep)
		}
	}

	if clock := formatClock(c.DurationS); clock != "" {
		b.WriteString("\n⏱ ")
		b.WriteString(clock)
	}
	if name := oneLine(c.FileName); name != "" {
		b.WriteString("\n📄 ")
		b.WriteString(name)
	}

	// Audio, subtitles and quality are all "how this file plays", so they share one line.
	var specs []string
	if a := strings.TrimSpace(c.AudioLangs); a != "" {
		specs = append(specs, "🔊 "+a)
	}
	if s := strings.TrimSpace(c.SubLangs); s != "" {
		specs = append(specs, "💬 "+s)
	}
	if q := strings.TrimSpace(c.Quality); q != "" {
		specs = append(specs, "🎞 "+q)
	}
	if len(specs) > 0 {
		b.WriteByte('\n')
		b.WriteString(strings.Join(specs, " · "))
	}

	return b.String()
}

// coordsLine is the last line: the identifiers needed to place this file again if the library
// database is gone. It returns the HTML to send and a plain rendering used only for the budget.
//
// Kind is not written as a token -- the /movie/ vs /tv/ segment of the TMDB link already carries
// it -- except when there is no TMDB id yet, where the bare branch word stands in for it.
//
// The first return is the HTML line to send; the second is its plain-text width for the budget.
func coordsLine(c StorageCaption) (string, string) {
	branch := "movie"
	if c.Kind == 2 {
		branch = "tv"
	}
	src := strings.TrimSpace(c.Source)
	if src == "" {
		src = "ox"
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

	fmt.Fprintf(&h, " · file <code>%d</code>", c.FileID)
	fmt.Fprintf(&p, " · file %d", c.FileID)

	if key := strings.TrimSpace(c.ExtKey); key != "" {
		fmt.Fprintf(&h, " · %s <code>%s</code>", esc(src), esc(key))
		fmt.Fprintf(&p, " · %s %s", src, key)
	}

	if link := sourceLink(c.SourceChannelID, c.SourceMsgID); link != "" {
		fmt.Fprintf(&h, ` · <a href="%s">Source</a>`, link)
		p.WriteString(" · Source")
	}

	return h.String(), p.String()
}

// sourceLink builds a t.me/c/ deep link to the exact source-channel post a file was indexed from.
// channelID is the Bot-API form (-100xxxxxxxxxx); t.me/c/ links use the id without that prefix and
// only resolve for someone already in the channel -- fine here, it's an internal jump link for
// whoever reads the clone/backup channel, not a public share link.
func sourceLink(channelID, msgID int64) string {
	if channelID == 0 || msgID == 0 {
		return ""
	}
	raw := strings.TrimPrefix(strconv.FormatInt(channelID, 10), "-100")
	return fmt.Sprintf("https://t.me/c/%s/%d", raw, msgID)
}

// esc replaces exactly the three characters Telegram's HTML mode reserves. Not html.EscapeString:
// this string is parsed by two different Telegram HTML parsers (Bot API and Pyrogram) and both
// only require < > &; escaping ' and " as well is needless here and one of them may balk.
var esc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace

func formatClock(seconds int32) string {
	if seconds <= 0 {
		return ""
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, s)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func clampRunes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		n := utf8.RuneLen(r)
		if n < 0 {
			n = 1
		}
		if b.Len()+n > maxBytes-3 {
			b.WriteRune('…')
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}
