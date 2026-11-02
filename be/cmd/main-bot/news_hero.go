package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/app/newsroom"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	newsHeroImageLimit  = 4 << 20
	newsHeroHTTPTimeout = 45 * time.Second
	newsHeroLogoSize    = "w500"
)

// composeNewsHero fetches TMDB poster (and title logo when we have one) and paints the 16:9 card.
// newsroom stays HTTP-free (R-GO-2); this is the thin cmd adapter the plan called for.
func composeNewsHero(ctx context.Context, a newsroom.Announcement) ([]byte, error) {
	return composeNewsHeroFetch(ctx, fetchNewsImage, a)
}

func composeNewsHeroFetch(
	ctx context.Context, get func(context.Context, string) ([]byte, error), a newsroom.Announcement,
) ([]byte, error) {
	posterURL := newsroom.PosterURL(a.Poster)
	if posterURL == "" {
		return nil, fmt.Errorf("empty poster")
	}
	poster, err := get(ctx, posterURL)
	if err != nil {
		return nil, err
	}
	var logo []byte
	if path := catalog.WireLogo(a.Logo); path != "" {
		if u := catalog.PosterURL(path, newsHeroLogoSize); u != "" {
			if b, logoErr := get(ctx, u); logoErr == nil {
				logo = b
			}
		}
	}
	return newsroom.FormatHeroJPEG(poster, logo, newsroom.HeroInputFrom(a))
}

func fetchNewsImage(ctx context.Context, imageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: newsHeroHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, newsHeroImageLimit))
	if err != nil {
		return nil, err
	}
	if len(data) < 128 {
		return nil, fmt.Errorf("image empty")
	}
	return data, nil
}

func newsHeroPost(ctx context.Context, a newsroom.Announcement) (string, []byte) {
	return richWithHero(a, mustComposeHero(ctx, a))
}

func newsHeroPreview(ctx context.Context, prev newsroom.StaffPreview) (string, []byte) {
	jpeg := mustComposeHero(ctx, prev.Announcement)
	if len(jpeg) == 0 {
		return prev.Rich, nil
	}
	return newsroom.FormatPreviewWithHero(prev.Announcement, prev.Locale), jpeg
}

func mustComposeHero(ctx context.Context, a newsroom.Announcement) []byte {
	jpeg, err := composeNewsHero(ctx, a)
	if err != nil || len(jpeg) == 0 {
		return nil
	}
	return jpeg
}

func richWithHero(a newsroom.Announcement, jpeg []byte) (string, []byte) {
	if len(jpeg) == 0 {
		return newsroom.FormatPost(a), nil
	}
	return newsroom.FormatPostWithHero(a), jpeg
}
