package newsroom

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

func drawHeroCinemaBackground(dst *image.RGBA) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	base := color.RGBA{0x0d, 0x0e, 0x09, 0xff} // nori-900
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, base)
		}
	}
	drawRadialGlowRect(dst, 0, 0, w, h, int(float64(w)*0.14), int(float64(h)*0.18), 300, color.RGBA{0xe3, 0x7a, 0x42, 0x30})
	drawRadialGlowRect(dst, 0, 0, w, h, w-int(float64(w)*0.12), int(float64(h)*0.14), 280, color.RGBA{0xe3, 0x8e, 0x5e, 0x28})
	drawRadialGlowRect(dst, 0, 0, w, h, int(float64(w)*0.52), h-int(float64(h)*0.08), 240, color.RGBA{0x6e, 0x8c, 0x3e, 0x18})
	drawHeroVignette(dst, 0.42)
}

func drawRadialGlowRect(dst *image.RGBA, minX, minY, maxX, maxY, cx, cy, radius int, core color.RGBA) {
	r2 := float64(radius * radius)
	y0 := maxInt(minY, cy-radius)
	y1 := minInt(maxY, cy+radius)
	x0 := maxInt(minX, cx-radius)
	x1 := minInt(maxX, cx+radius)
	for y := y0; y < y1; y++ {
		dy := float64(y - cy)
		for x := x0; x < x1; x++ {
			dx := float64(x - cx)
			d2 := dx*dx + dy*dy
			if d2 > r2 {
				continue
			}
			t := 1.0 - d2/r2
			alpha := uint8(float64(core.A) * t * t)
			dst.Set(x, y, blendOver(dst.At(x, y), color.RGBA{core.R, core.G, core.B, alpha}))
		}
	}
}

func drawHeroVignette(dst *image.RGBA, strength float64) {
	if strength <= 0 {
		return
	}
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	cx, cy := float64(w-1)/2, float64(h-1)/2
	maxR := math.Hypot(cx, cy)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy) / maxR
			if d <= 0.55 {
				continue
			}
			t := (d - 0.55) / 0.45
			if t > 1 {
				t = 1
			}
			alpha := uint8(strength * t * 255)
			dst.Set(x, y, blendOver(dst.At(x, y), color.RGBA{0x00, 0x00, 0x00, alpha}))
		}
	}
}

func drawHeroStar(dst *image.RGBA, cx, cy, outerR int, col color.Color) {
	if outerR <= 0 {
		return
	}
	innerR := float64(outerR) * 0.42
	pts := make([][2]float64, 10)
	for i := 0; i < 10; i++ {
		angle := -math.Pi/2 + float64(i)*math.Pi/5
		r := float64(outerR)
		if i%2 == 1 {
			r = innerR
		}
		pts[i] = [2]float64{
			float64(cx) + r*math.Cos(angle),
			float64(cy) + r*math.Sin(angle),
		}
	}
	minX, maxX := cx-outerR-1, cx+outerR+1
	minY, maxY := cy-outerR-1, cy+outerR+1
	for py := minY; py <= maxY; py++ {
		for px := minX; px <= maxX; px++ {
			if pointInPolygon(float64(px)+0.5, float64(py)+0.5, pts) {
				dst.Set(px, py, col)
			}
		}
	}
}

func pointInPolygon(x, y float64, poly [][2]float64) bool {
	inside := false
	j := len(poly) - 1
	for i := 0; i < len(poly); i++ {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if ((yi > y) != (yj > y)) && (x < (xj-xi)*(y-yi)/(yj-yi)+xi) {
			inside = !inside
		}
		j = i
	}
	return inside
}

func drawHeroTextBlockCentered(dst *image.RGBA, text string, centerX, y, maxW int, face font.Face, col color.Color) int {
	lines := wrapHeroText(text, face, maxW)
	lineH := heroLineHeight(face)
	for _, line := range lines {
		w := textWidth(line, face)
		drawHeroText(dst, line, centerX-w/2, y+lineH, face, col)
		y += lineH
	}
	return y
}

func measureTextBlockHeight(text string, face font.Face, maxW int) int {
	lines := wrapHeroText(text, face, maxW)
	return len(lines) * heroLineHeight(face)
}

func heroLineHeight(face font.Face) int {
	return face.Metrics().Height.Ceil() + 6
}

func drawGradientText(dst *image.RGBA, text string, x, y int, face font.Face) {
	start := color.RGBA{0xe3, 0x7a, 0x42, 0xff} // salmon
	end := color.RGBA{0xe3, 0x8e, 0x5e, 0xff}   // salmon-light
	runes := []rune(text)
	n := len(runes)
	for i, r := range runes {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		s := string(r)
		drawHeroText(dst, s, x, y, face, lerpColor(start, end, t))
		x += textWidth(s, face)
	}
}

func drawHeroText(dst *image.RGBA, text string, x, y int, face font.Face, col color.Color) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func wrapHeroText(text string, face font.Face, maxW int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		candidate := w
		if cur.Len() > 0 {
			candidate = strings.TrimSpace(cur.String() + " " + w)
		}
		if textWidth(candidate, face) <= maxW || cur.Len() == 0 {
			cur.Reset()
			cur.WriteString(candidate)
			continue
		}
		lines = append(lines, strings.TrimSpace(cur.String()))
		cur.Reset()
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, strings.TrimSpace(cur.String()))
	}
	return lines
}

func textWidth(text string, face font.Face) int {
	d := &font.Drawer{Face: face}
	return d.MeasureString(text).Ceil()
}

func resizeSmooth(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst
}

func drawContainSmoothInRect(dst *image.RGBA, src image.Image, rect image.Rectangle) {
	dw, dh := rect.Dx(), rect.Dy()
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	scale := minFloat(float64(dw)/float64(sw), float64(dh)/float64(sh))
	nw := maxInt(1, int(float64(sw)*scale+0.5))
	nh := maxInt(1, int(float64(sh)*scale+0.5))
	scaled := resizeSmooth(src, nw, nh)
	x0 := rect.Min.X + (dw-nw)/2
	y0 := rect.Min.Y + (dh-nh)/2
	draw.Draw(dst, image.Rect(x0, y0, x0+nw, y0+nh), scaled, image.Point{}, draw.Over)
}

// drawContainRightInRect fits src inside rect and pins it to the right edge.
func drawContainRightInRect(dst *image.RGBA, src image.Image, rect image.Rectangle) image.Rectangle {
	dw, dh := rect.Dx(), rect.Dy()
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	scale := minFloat(float64(dw)/float64(sw), float64(dh)/float64(sh))
	nw := maxInt(1, int(float64(sw)*scale+0.5))
	nh := maxInt(1, int(float64(sh)*scale+0.5))
	scaled := resizeSmooth(src, nw, nh)
	x0 := rect.Max.X - nw
	y0 := rect.Min.Y + (dh-nh)/2
	drawn := image.Rect(x0, y0, x0+nw, y0+nh)
	draw.Draw(dst, drawn, scaled, image.Point{}, draw.Over)
	return drawn
}

func colorWhite() color.Color   { return color.RGBA{0xff, 0xff, 0xff, 0xff} }
func colorMuted() color.Color   { return color.RGBA{0xc8, 0xc8, 0xd4, 0xff} }
func colorPrimary() color.Color { return color.RGBA{0xe3, 0x7a, 0x42, 0xff} } // salmon

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 0xff,
	}
}

func blendOver(dst color.Color, src color.RGBA) color.RGBA {
	dr, dg, db, _ := dst.RGBA()
	sa := float64(src.A) / 255
	inv := 1 - sa
	return color.RGBA{
		R: uint8(float64(src.R)*sa + float64(dr>>8)*inv),
		G: uint8(float64(src.G)*sa + float64(dg>>8)*inv),
		B: uint8(float64(src.B)*sa + float64(db>>8)*inv),
		A: 0xff,
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
