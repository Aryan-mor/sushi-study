package newsroom

import (
	"bytes"
	"fmt"
	"image"
	"strings"

	"golang.org/x/image/font"
)

type heroBlock struct {
	kind     string
	text     string
	height   int
	isRating bool
	logo     image.Image
}

func drawHeroLeftPanel(dst *image.RGBA, titleLogo image.Image, in HeroInput, leftW int) error {
	panelH := dst.Bounds().Dy()
	padX := 28
	maxTextW := leftW - padX*2
	if maxTextW < 120 {
		maxTextW = 120
	}
	blocks, err := collectHeroBlocks(titleLogo, in, maxTextW)
	if err != nil {
		return err
	}
	totalH := heroStackHeight(blocks)
	y := (panelH - totalH) / 2
	if y < 16 {
		y = 16
	}
	return paintHeroBlocks(dst, blocks, in, leftW/2, y, maxTextW)
}

func collectHeroBlocks(titleLogo image.Image, in HeroInput, maxTextW int) ([]heroBlock, error) {
	var blocks []heroBlock
	if in.SeasonNum > 0 && in.EpisodeNum > 0 {
		face, err := heroFace(26, true)
		if err != nil {
			return nil, err
		}
		line := heroEpisodeLine(in)
		blocks = append(blocks, heroBlock{kind: "episode", text: line, height: measureTextBlockHeight(line, face, maxTextW)})
	}
	if titleLogo != nil {
		blocks = append(blocks, heroBlock{kind: "logo", height: 128, logo: titleLogo})
	} else if title := strings.TrimSpace(in.TitleEN); title != "" {
		face, err := heroFace(32, true)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, heroBlock{kind: "title", text: title, height: measureTextBlockHeight(title, face, maxTextW)})
	}
	meta, err := heroMetaBlocks(in, maxTextW)
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, meta...)
	return append(blocks, heroBlock{kind: "brand", height: 56}), nil
}

func heroMetaBlocks(in HeroInput, maxTextW int) ([]heroBlock, error) {
	var blocks []heroBlock
	for _, line := range heroMetaLines(in) {
		face, err := heroFace(20, false)
		if err != nil {
			return nil, err
		}
		isRating := strings.TrimSpace(in.VoteLabel) != "" && line == heroRatingLine(in)
		blocks = append(blocks, heroBlock{
			kind: "meta", text: line, height: measureTextBlockHeight(line, face, maxTextW), isRating: isRating,
		})
	}
	return blocks, nil
}

func heroStackHeight(blocks []heroBlock) int {
	totalH := 0
	for i, b := range blocks {
		totalH += b.height
		if i < len(blocks)-1 {
			totalH += gapAfter(b.kind, blocks[i+1].kind)
		}
	}
	return totalH
}

func paintHeroBlocks(dst *image.RGBA, blocks []heroBlock, in HeroInput, centerX, y, maxTextW int) error {
	for i, b := range blocks {
		nextY, err := paintHeroBlock(dst, b, in, centerX, y, maxTextW)
		if err != nil {
			return err
		}
		y = nextY
		if i < len(blocks)-1 {
			y += gapAfter(b.kind, blocks[i+1].kind)
		}
	}
	return nil
}

func paintHeroBlock(dst *image.RGBA, b heroBlock, in HeroInput, centerX, y, maxTextW int) (int, error) {
	switch b.kind {
	case "episode":
		return paintHeroEpisode(dst, b, centerX, y, maxTextW)
	case "logo":
		drawContainSmoothInRect(dst, b.logo, image.Rect(centerX-maxTextW/2, y, centerX+maxTextW/2, y+128))
		return y + 128, nil
	case "title":
		face, err := heroFace(32, true)
		if err != nil {
			return y, err
		}
		return drawHeroTextBlockCentered(dst, b.text, centerX, y, maxTextW, face, colorWhite()), nil
	case "meta":
		return paintHeroMeta(dst, b, in, centerX, y, maxTextW)
	case "brand":
		drawHeroBrandCentered(dst, centerX, y)
		return y + 56, nil
	default:
		return y, nil
	}
}

func paintHeroEpisode(dst *image.RGBA, b heroBlock, centerX, y, maxTextW int) (int, error) {
	face, err := heroFace(26, true)
	if err != nil {
		return y, err
	}
	drawY := y - 20
	if drawY < 8 {
		drawY = 8
	}
	_ = drawHeroTextBlockCentered(dst, b.text, centerX, drawY, maxTextW, face, colorPrimary())
	return y + b.height, nil
}

func paintHeroMeta(dst *image.RGBA, b heroBlock, in HeroInput, centerX, y, maxTextW int) (int, error) {
	face, err := heroFace(20, false)
	if err != nil {
		return y, err
	}
	if b.isRating {
		return drawHeroRatingLine(dst, in.VoteLabel, centerX, y, face), nil
	}
	return drawHeroTextBlockCentered(dst, b.text, centerX, y, maxTextW, face, colorMuted()), nil
}

func gapAfter(kind, next string) int {
	if next == "brand" {
		return 28
	}
	switch kind {
	case "logo", "title", "episode":
		return 18
	case "meta":
		return 10
	default:
		return 28
	}
}

func drawHeroBrandCentered(dst *image.RGBA, centerX, y int) {
	logoImg, _, err := image.Decode(bytes.NewReader(sushiLogoPNG))
	if err != nil {
		return
	}
	const logoSize = 52
	const word = "Sushi"
	face, err := heroFace(28, true)
	if err != nil {
		return
	}
	wordW := textWidth(word, face)
	gap := 12
	startX := centerX - (logoSize+gap+wordW)/2
	drawContainSmoothInRect(dst, logoImg, image.Rect(startX, y, startX+logoSize, y+logoSize))
	drawGradientText(dst, word, startX+logoSize+gap, heroTextBaselineInRow(y, logoSize, face), face)
}

func heroTextBaselineInRow(rowTop, rowH int, face font.Face) int {
	m := face.Metrics()
	return rowTop + (rowH+m.Ascent.Ceil()-m.Descent.Ceil())/2
}

func heroEpisodeLine(in HeroInput) string {
	return fmt.Sprintf("Season %d · Episode %d", in.SeasonNum, in.EpisodeNum)
}

func heroRatingLine(in HeroInput) string {
	return strings.TrimSpace(in.VoteLabel)
}

func heroMetaLines(in HeroInput) []string {
	var lines []string
	if v := strings.TrimSpace(in.VoteLabel); v != "" {
		lines = append(lines, v)
	}
	var mid []string
	if g := strings.TrimSpace(in.GenresEN); g != "" {
		mid = append(mid, g)
	}
	if y := strings.TrimSpace(in.YearRange); y != "" {
		mid = append(mid, y)
	}
	if len(mid) > 0 {
		lines = append(lines, strings.Join(mid, " · "))
	}
	var tail []string
	if r := strings.TrimSpace(in.RuntimeEN); r != "" {
		tail = append(tail, r)
	}
	if a := strings.TrimSpace(in.OfficialRating); a != "" {
		tail = append(tail, a)
	}
	if len(tail) > 0 {
		lines = append(lines, strings.Join(tail, " · "))
	}
	return lines
}

func drawHeroRatingLine(dst *image.RGBA, vote string, centerX, y int, face font.Face) int {
	vote = strings.TrimSpace(vote)
	if vote == "" {
		return y
	}
	const gap, starR = 8, 10
	starBox := starR*2 + 2
	totalW := starBox + gap + textWidth(vote, face)
	startX := centerX - totalW/2
	lineH := heroLineHeight(face)
	drawHeroStar(dst, startX+starR+1, y+lineH/2, starR, colorPrimary())
	drawHeroText(dst, vote, startX+starBox+gap, heroTextBaselineInRow(y, lineH, face), face, colorMuted())
	return y + lineH
}
