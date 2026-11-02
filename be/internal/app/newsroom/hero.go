package newsroom

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// Hero card is the prototype's FormatChannelNewsHeroJPEG, split to stay under R-GO-17 and rebranded
// (ADR 0017 §6). 16:9, poster flush right, metadata left, Sushi wordmark at the foot. No R2.

const (
	// HeroWidth and HeroHeight are Telegram's 16:9 slideshow frame.
	HeroWidth  = 1280
	HeroHeight = 720
	// HeroJPEGMaxBytes keeps the attach:// part small enough that sendRichMessage stays cheap.
	HeroJPEGMaxBytes = 160_000
	heroJPEGQuality  = 82
	// HeroMediaID is the attach:// / tg://photo id SendRichKeyboard and FormatPostWithHero share.
	HeroMediaID = "hero"
)

// HeroInput is English-facing metadata drawn on the card. The channel caption stays Persian.
type HeroInput struct {
	TitleEN        string
	VoteLabel      string
	GenresEN       string
	YearRange      string
	RuntimeEN      string
	OfficialRating string
	SeasonNum      int // episode announcements: >0 with EpisodeNum draws "Season N · Episode M"
	EpisodeNum     int
}

var heroFonts struct {
	once    sync.Once
	regular *opentype.Font
	bold    *opentype.Font
	err     error
}

func loadHeroFonts() (*opentype.Font, *opentype.Font, error) {
	heroFonts.once.Do(func() {
		heroFonts.regular, heroFonts.err = opentype.Parse(vazirmatnRegularTTF)
		if heroFonts.err != nil {
			return
		}
		heroFonts.bold, heroFonts.err = opentype.Parse(vazirmatnBoldTTF)
	})
	return heroFonts.regular, heroFonts.bold, heroFonts.err
}

func heroFace(size float64, bold bool) (font.Face, error) {
	reg, bld, err := loadHeroFonts()
	if err != nil {
		return nil, err
	}
	f := reg
	if bold {
		f = bld
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 96})
}

// FormatHeroJPEG builds a 16:9 card: nori cinema background, poster flush right, metadata left.
func FormatHeroJPEG(posterBytes, titleLogoBytes []byte, in HeroInput) ([]byte, error) {
	poster, _, err := image.Decode(bytes.NewReader(posterBytes))
	if err != nil {
		return nil, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, HeroWidth, HeroHeight))
	drawHeroCinemaBackground(dst)

	posterDrawn := drawContainRightInRect(dst, poster, dst.Bounds())
	leftW := posterDrawn.Min.X
	if leftW < 280 {
		leftW = heroLeftPanelWidth()
	}

	var titleLogo image.Image
	if len(titleLogoBytes) > 0 {
		if img, _, decErr := image.Decode(bytes.NewReader(titleLogoBytes)); decErr == nil {
			titleLogo = img
		}
	}
	if err := drawHeroLeftPanel(dst, titleLogo, in, leftW); err != nil {
		return nil, err
	}
	return encodeHeroJPEG(dst)
}

func encodeHeroJPEG(img image.Image) ([]byte, error) {
	quality := heroJPEGQuality
	var out bytes.Buffer
	for range 5 {
		out.Reset()
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
		if out.Len() <= HeroJPEGMaxBytes || quality <= 68 {
			return append([]byte(nil), out.Bytes()...), nil
		}
		quality -= 6
	}
	return append([]byte(nil), out.Bytes()...), nil
}

func heroLeftPanelWidth() int {
	return HeroWidth * 44 / 100
}

// HeroInputFrom maps a queued announcement onto the English card fields. Genres stay as stored
// (TMDB English names). Episode line is English; channel caption stays Persian.
func HeroInputFrom(a Announcement) HeroInput {
	in := HeroInput{TitleEN: strings.TrimSpace(a.Title)}
	if a.Rating > 0 {
		in.VoteLabel = fmt.Sprintf("%.1f/10", float64(a.Rating)/10)
	}
	if names := catalog.SplitGenres(a.Genres); len(names) > 0 {
		if len(names) > maxGenres {
			names = names[:maxGenres]
		}
		in.GenresEN = strings.Join(names, ", ")
	}
	if a.Year > 0 {
		in.YearRange = fmt.Sprintf("%d", a.Year)
	}
	if mins := a.RuntimeS / 60; mins > 0 {
		in.RuntimeEN = fmt.Sprintf("%d min", mins)
	}
	if a.Kind == catalog.KindSeries && a.Season > 0 && a.Episode > 0 {
		in.SeasonNum = int(a.Season)
		in.EpisodeNum = int(a.Episode)
	}
	return in
}
