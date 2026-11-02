package catalog

import (
	"regexp"
	"strconv"
	"strings"
)

// Delivery tokens stored on quality_label so a picker can tell HardSub / SoftSub / Dub apart
// even when audio_langs / sub_langs were never probed. Tech() stays resolution+codec+source;
// EnrichLabel appends the delivery token.
const (
	DeliveryHardSub = "HardSub"
	DeliverySoftSub = "SoftSub"
	DeliveryDub     = "Dub"
)

// FileSpec is the technical side of a file pick: resolution, codec, source, delivery. Language is
// separate (AudioLangs / SubLangs) so a button can show "1080p x265 · dubbed · 412 MB".
type FileSpec struct {
	Height   int32
	Codec    string
	Source   string
	Bit10    bool
	Delivery string
}

var (
	// Delimiters are `[^a-z0-9]` (not `\b`) so `_720p_` / `.Hdrip.Duble.` scene names match.
	// Underscore is a word character; `\b` silently misses those tokens.
	reSpecResP    = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(\d{3,4})p(?:$|[^a-z0-9])`)
	reSpecResBare = regexp.MustCompile(`(?i)(?:^|[^0-9])(2160|1080|720|480)(?:\s|$|[^0-9pP])`)
	reSpec4K      = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(4k|uhd|2160p)(?:$|[^a-z0-9])`)
	reSpecCodec   = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(x265|h\.?265|hevc|x264|h\.?264|avc|av1)(?:$|[^a-z0-9])`)
	reSpecSource  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(web-?dl|webrip|blu-?ray|bluray|brrip|bdrip|hdtv|remux|hdrip|hdts)(?:$|[^a-z0-9])`)
	reSpec10Bit   = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])10[\s._-]?bit(?:$|[^a-z0-9])`)
	// چسبیده / زیرنویس_چسبیده is a Persian-sub *claim*, not a hard vs soft fact.
	// Container tracks (sub_langs / srt) decide; see ResolveDelivery.
	reSpecHardSub  = regexp.MustCompile(`(?i)(hard[\s._-]*sub|هارد[\s_\x{200c}-]*ساب|burned[\s_-]*in)`)
	reSpecSoftSub  = regexp.MustCompile(`(?i)(soft[\s._-]*sub|سافت[\s_\x{200c}-]*ساب)`)
	reSpecSubClaim = regexp.MustCompile(`(?i)(چسبیده|زیرنویس|هاردساب|سافت)`)
	reSpecDub      = regexp.MustCompile(`(?i)((?:^|[^a-z0-9])(?:dub(?:bed)?|duble)(?:$|[^a-z0-9])|دوبله)`)
	reSpecTheater  = regexp.MustCompile(`پرده`)
	reCaptionQual  = regexp.MustCompile(`(?i)کیفیت\s*[:：]?\s*(\d{3,4})\s*p`)
	reConverted    = regexp.MustCompile(`(?i)video[_\s-]*converter`)
)

// ParseFileSpec reads resolution / codec / source / delivery tokens from filenames, the prototype
// labels, and clone captions. Height 0 and empty strings are ignored. A year (2018) is not a
// resolution. "زیرنویس چسبیده" is not HardSub — only explicit HardSub / هاردساب / burned-in.
func ParseFileSpec(blobs ...string) FileSpec {
	return parseOne(strings.Join(blobs, " "))
}

// ParseReleaseSpec is the ingest path: filename tokens win, caption fills gaps. A leftover
// "چسبیده" in a quoted album caption must not relabel a Duble file as HardSub. A single کیفیت Np
// wins height when the filename is a video_converter downscale that kept the original 720p token.
func ParseReleaseSpec(fileName, caption string) FileSpec {
	fromFile := parseOne(fileName)
	fromCap := parseOne(caption)
	out := mergeSpec(fromFile, fromCap)
	if out.Delivery == "" && captionDeliveryOK(caption) {
		out.Delivery = fromCap.Delivery
	}
	if h, ok := uniqueCaptionQuality(caption); ok {
		if reConverted.MatchString(fileName) || out.Height == 0 {
			out.Height = h
		}
	}
	return out
}

func parseOne(blob string) FileSpec {
	if strings.TrimSpace(blob) == "" {
		return FileSpec{}
	}
	var s FileSpec
	s.Height = specHeight(blob)
	s.Codec = specCodec(blob)
	s.Source = specSource(blob)
	s.Bit10 = reSpec10Bit.MatchString(blob)
	s.Delivery = specDelivery(blob)
	return s
}

// mergeSpec copies empty fields from extra onto base. Delivery is filename-only here — caption
// delivery is applied in ParseReleaseSpec after the dub+hardsub ambiguity check.
func mergeSpec(base, extra FileSpec) FileSpec {
	out := base
	if out.Height == 0 {
		out.Height = extra.Height
	}
	if out.Codec == "" {
		out.Codec = extra.Codec
	}
	if out.Source == "" {
		out.Source = extra.Source
	}
	if !out.Bit10 {
		out.Bit10 = extra.Bit10
	}
	return out
}

func captionDeliveryOK(caption string) bool {
	return !(reSpecHardSub.MatchString(caption) && reSpecDub.MatchString(caption))
}

func uniqueCaptionQuality(caption string) (int32, bool) {
	ms := reCaptionQual.FindAllStringSubmatch(caption, -1)
	if len(ms) == 0 {
		return 0, false
	}
	var h int32
	for _, m := range ms {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 144 || n > 4320 {
			return 0, false
		}
		if h == 0 {
			h = int32(n)
			continue
		}
		if int32(n) != h {
			return 0, false
		}
	}
	return h, h > 0
}

// Resolution is the user-facing height token: "1080p", "720p", "4K".
func (s FileSpec) Resolution() string {
	switch {
	case s.Height >= 2160:
		return "4K"
	case s.Height > 0:
		return strconv.Itoa(int(s.Height)) + "p"
	default:
		return ""
	}
}

// Tech is the compact picker token, e.g. "1080p x265" or "720p WEB-DL".
func (s FileSpec) Tech() string {
	var parts []string
	if r := s.Resolution(); r != "" {
		parts = append(parts, r)
	}
	if s.Codec != "" {
		parts = append(parts, s.Codec)
	}
	if s.Bit10 {
		parts = append(parts, "10bit")
	}
	if s.Source != "" {
		parts = append(parts, s.Source)
	}
	return strings.Join(parts, " ")
}

// EnrichLabel keeps an existing quality_label (the prototype "720p Dub WEB-DL") and appends codec /
// 10bit / delivery when the filename knew them and the label did not.
func (s FileSpec) EnrichLabel(existing string) string {
	existing = strings.TrimSpace(existing)
	if existing == "" {
		existing = s.Tech()
	}
	blob := strings.ToLower(existing)
	var extra []string
	if s.Codec != "" && !containsCodec(blob, s.Codec) {
		extra = append(extra, s.Codec)
	}
	if s.Bit10 && !strings.Contains(blob, "10bit") && !strings.Contains(blob, "10-bit") {
		extra = append(extra, "10bit")
	}
	if s.Source != "" && specSource(existing) == "" {
		extra = append(extra, s.Source)
	}
	if s.Delivery != "" && !containsDelivery(blob, s.Delivery) {
		extra = append(extra, s.Delivery)
	}
	if len(extra) == 0 {
		return existing
	}
	if existing == "" {
		return strings.Join(extra, " ")
	}
	return strings.TrimSpace(existing + " " + strings.Join(extra, " "))
}

// ResolveDelivery is the picker token after looking at the file, not just the dump caption.
// sub_langs is the embedded srt/ass tracks (docs/12 §5.1). A non-empty value means the container
// has a soft sub — that wins over a leftover HardSub on the label. Empty sub_langs is unknown,
// never "none"; HardSub then only comes from an explicit token (HardSub.mkv / هاردساب).
func ResolveDelivery(spec FileSpec, audioLangs, subLangs string) string {
	if spec.Delivery == DeliveryDub {
		return DeliveryDub
	}
	if strings.TrimSpace(subLangs) != "" {
		return DeliverySoftSub
	}
	if spec.Delivery == DeliveryHardSub {
		return DeliveryHardSub
	}
	if HasLang(audioLangs, "fa") {
		return DeliveryDub
	}
	if spec.Delivery == DeliverySoftSub {
		return DeliverySoftSub
	}
	return ""
}

// ClaimsPersianSub reports a dump caption/filename that says the file has Persian subs, without
// saying whether they are muxed srt or burned in. Used by the player once it can see tracks.
func ClaimsPersianSub(blob string) bool {
	return reSpecSubClaim.MatchString(blob) || reSpecHardSub.MatchString(blob) || reSpecSoftSub.MatchString(blob)
}

func specDelivery(blob string) string {
	switch {
	case reSpecHardSub.MatchString(blob):
		return DeliveryHardSub
	case reSpecDub.MatchString(blob):
		return DeliveryDub
	case reSpecSoftSub.MatchString(blob):
		return DeliverySoftSub
	default:
		return ""
	}
}

func containsDelivery(blob, delivery string) bool {
	switch delivery {
	case DeliveryHardSub:
		return reSpecHardSub.MatchString(blob)
	case DeliverySoftSub:
		return reSpecSoftSub.MatchString(blob)
	case DeliveryDub:
		return reSpecDub.MatchString(blob)
	default:
		return false
	}
}

func specHeight(blob string) int32 {
	if reSpec4K.MatchString(blob) {
		if m := reSpecResP.FindStringSubmatch(blob); len(m) > 1 {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 2160 {
				return 2160
			}
		}
		return 2160
	}
	if m := reSpecResP.FindStringSubmatch(blob); len(m) > 1 {
		n, err := strconv.Atoi(m[1])
		if err == nil && n >= 144 && n <= 4320 {
			return int32(n)
		}
	}
	if m := reSpecResBare.FindStringSubmatch(blob); len(m) > 1 {
		n, err := strconv.Atoi(m[1])
		if err == nil && n >= 480 && n <= 2160 {
			return int32(n)
		}
	}
	return 0
}

func specCodec(blob string) string {
	m := reSpecCodec.FindStringSubmatch(blob)
	if len(m) < 2 {
		return ""
	}
	tok := strings.ToLower(strings.ReplaceAll(m[1], ".", ""))
	switch tok {
	case "x265", "h265", "hevc":
		return "x265"
	case "x264", "h264", "avc":
		return "x264"
	case "av1":
		return "AV1"
	default:
		return ""
	}
}

func specSource(blob string) string {
	if reSpecTheater.MatchString(blob) {
		return "HDTS"
	}
	m := reSpecSource.FindStringSubmatch(blob)
	if len(m) < 2 {
		return ""
	}
	tok := strings.ToLower(strings.ReplaceAll(m[1], "-", ""))
	switch {
	case strings.Contains(tok, "webdl"):
		return "WEB-DL"
	case strings.Contains(tok, "webrip"):
		return "WEBRip"
	case strings.Contains(tok, "blu"), strings.Contains(tok, "brrip"), strings.Contains(tok, "bdrip"):
		return "BluRay"
	case strings.Contains(tok, "hdtv"):
		return "HDTV"
	case strings.Contains(tok, "remux"):
		return "REMUX"
	case tok == "hdrip":
		return "HDRip"
	case tok == "hdts":
		return "HDTS"
	default:
		return ""
	}
}

func containsCodec(blob, codec string) bool {
	c := strings.ToLower(codec)
	switch c {
	case "x265":
		return strings.Contains(blob, "x265") || strings.Contains(blob, "h265") || strings.Contains(blob, "hevc")
	case "x264":
		return strings.Contains(blob, "x264") || strings.Contains(blob, "h264") || strings.Contains(blob, "avc")
	case "av1":
		return strings.Contains(blob, "av1")
	default:
		return strings.Contains(blob, c)
	}
}
