package cmd

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// getTrayIcons returns the template icon (for macOS menu bar) and regular colored icon (for Linux/Windows).
func getTrayIcons() (templateIcon []byte, regularIcon []byte) {
	templateIcon = generateQuokkaIcon(true)
	regularIcon = generateQuokkaIcon(false)
	return templateIcon, regularIcon
}

// generateQuokkaIcon draws a 32x32 icon of the aiquokka mascot.
// If isTemplate is true, it renders monochrome white-with-alpha suitable for macOS NSImage template.
// If isTemplate is false, it renders a friendly warm-colored quokka with vibrant accents.
func generateQuokkaIcon(isTemplate bool) []byte {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Colors
	var (
		cHead     color.Color
		cEarInner color.Color
		cFeature  color.Color
	)

	if isTemplate {
		// Template icon: pure white with varying alpha for anti-aliasing
		cHead = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		cEarInner = color.RGBA{R: 0, G: 0, B: 0, A: 0} // transparent cutout
		cFeature = color.RGBA{R: 0, G: 0, B: 0, A: 0}  // transparent cutouts for eyes/nose/smile
	} else {
		// Colored icon: warm quokka brown and cute features
		cHead = color.RGBA{R: 196, G: 138, B: 90, A: 255}      // warm fur
		cEarInner = color.RGBA{R: 235, G: 180, B: 155, A: 255} // soft inner ear
		cFeature = color.RGBA{R: 50, G: 35, B: 25, A: 255}     // dark eyes & nose
	}

	drawFilledCircle := func(cx, cy, r float64, c color.Color) {
		r2 := r * r
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dx := float64(x) + 0.5 - cx
				dy := float64(y) + 0.5 - cy
				dist2 := dx*dx + dy*dy
				if dist2 <= r2 {
					img.Set(x, y, c)
				}
			}
		}
	}

	// 1. Draw outer ears
	drawFilledCircle(9, 8, 4.2, cHead)
	drawFilledCircle(23, 8, 4.2, cHead)

	// 2. Draw inner ears
	drawFilledCircle(9, 8, 2.3, cEarInner)
	drawFilledCircle(23, 8, 2.3, cEarInner)

	// 3. Draw head (chubby quokka cheeks)
	drawFilledCircle(16, 17, 9.8, cHead)
	drawFilledCircle(11, 19, 5.5, cHead) // left cheek
	drawFilledCircle(21, 19, 5.5, cHead) // right cheek

	// 4. Features: Eyes
	drawFilledCircle(12.5, 14.5, 1.4, cFeature)
	drawFilledCircle(19.5, 14.5, 1.4, cFeature)

	// Eye highlights (if colored)
	if !isTemplate {
		img.Set(12, 14, color.RGBA{R: 255, G: 255, B: 255, A: 220})
		img.Set(19, 14, color.RGBA{R: 255, G: 255, B: 255, A: 220})
	}

	// 5. Nose (cute triangle/oval at center)
	drawFilledCircle(16, 17.5, 1.7, cFeature)

	// 6. Quokka smile
	for _, pt := range []image.Point{
		{16, 20},
		{15, 21}, {17, 21},
		{14, 21}, {18, 21},
		{13, 20}, {19, 20},
	} {
		img.Set(pt.X, pt.Y, cFeature)
	}

	// 7. Small status lightning spark at bottom-right corner for tech vibe
	sparkColor := color.RGBA{R: 0, G: 210, B: 255, A: 255}
	if isTemplate {
		sparkColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	drawSpark(img, 27, 26, sparkColor)

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func drawSpark(dst draw.Image, cx, cy int, c color.Color) {
	// Tiny 5x5 lightning spark
	spark := [][2]int{
		{1, -2},
		{0, -1}, {1, -1},
		{-1, 0}, {0, 0}, {1, 0},
		{-1, 1}, {0, 1},
		{-1, 2},
	}
	for _, p := range spark {
		x := cx + p[0]
		y := cy + p[1]
		if x >= 0 && x < 32 && y >= 0 && y < 32 {
			dst.Set(x, y, c)
		}
	}
}
