package cmd

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

const (
	glyphUnits      = 16.0
	glyphPixels     = 32
	glyphSubsamples = 4
)

var (
	templateInk = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	regularInk  = color.NRGBA{R: 0x8a, G: 0x8a, B: 0x8e, A: 255}
)

type shape func(x, y float64) float64

type glyph struct {
	template []byte
	regular  []byte
}

func makeGlyph(s shape) glyph {
	return glyph{template: rasterize(s, templateInk), regular: rasterize(s, regularInk)}
}

func rasterize(s shape, ink color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, glyphPixels, glyphPixels))
	scale := glyphUnits / glyphPixels
	step := 1.0 / glyphSubsamples
	for py := 0; py < glyphPixels; py++ {
		for px := 0; px < glyphPixels; px++ {
			var sum float64
			for sy := 0; sy < glyphSubsamples; sy++ {
				for sx := 0; sx < glyphSubsamples; sx++ {
					x := (float64(px) + (float64(sx)+0.5)*step) * scale
					y := (float64(py) + (float64(sy)+0.5)*step) * scale
					sum += s(x, y)
				}
			}
			a := sum / (glyphSubsamples * glyphSubsamples)
			if a <= 0 {
				continue
			}
			c := ink
			c.A = uint8(math.Round(float64(ink.A) * math.Min(a, 1)))
			img.SetNRGBA(px, py, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func union(shapes ...shape) shape {
	return func(x, y float64) float64 {
		var best float64
		for _, s := range shapes {
			if v := s(x, y); v > best {
				best = v
			}
		}
		return best
	}
}

func minus(a, b shape) shape {
	return func(x, y float64) float64 {
		if b(x, y) > 0 {
			return 0
		}
		return a(x, y)
	}
}

func faded(s shape, alpha float64) shape {
	return func(x, y float64) float64 { return s(x, y) * alpha }
}

func empty(float64, float64) float64 { return 0 }

func inside(ok bool) float64 {
	if ok {
		return 1
	}
	return 0
}

func disc(cx, cy, r float64) shape {
	return func(x, y float64) float64 { return inside(math.Hypot(x-cx, y-cy) <= r) }
}

func turnOf(dx, dy float64) float64 {
	t := math.Atan2(dx, -dy) / (2 * math.Pi)
	if t < 0 {
		t++
	}
	return t
}

func arc(cx, cy, rIn, rOut, from, to float64) shape {
	return func(x, y float64) float64 {
		d := math.Hypot(x-cx, y-cy)
		if d < rIn || d > rOut {
			return 0
		}
		t := turnOf(x-cx, y-cy)
		return inside(t >= from && t <= to)
	}
}

func ring(cx, cy, rIn, rOut float64) shape {
	return arc(cx, cy, rIn, rOut, 0, 1)
}

func segment(x1, y1, x2, y2, width float64) shape {
	return func(x, y float64) float64 {
		dx, dy := x2-x1, y2-y1
		t := 0.0
		if l2 := dx*dx + dy*dy; l2 > 0 {
			t = math.Max(0, math.Min(1, ((x-x1)*dx+(y-y1)*dy)/l2))
		}
		return inside(math.Hypot(x-(x1+t*dx), y-(y1+t*dy)) <= width/2)
	}
}

func triangle(ax, ay, bx, by, cx, cy float64) shape {
	return func(x, y float64) float64 {
		d1 := (x-bx)*(ay-by) - (ax-bx)*(y-by)
		d2 := (x-cx)*(by-cy) - (bx-cx)*(y-cy)
		d3 := (x-ax)*(cy-ay) - (cx-ax)*(y-ay)
		neg := d1 < 0 || d2 < 0 || d3 < 0
		pos := d1 > 0 || d2 > 0 || d3 > 0
		return inside(!(neg && pos))
	}
}

func pointOnCircle(cx, cy, r, turn float64) (float64, float64) {
	a := turn * 2 * math.Pi
	return cx + r*math.Sin(a), cy - r*math.Cos(a)
}

func exclamation(cx, top, bottom float64) shape {
	return union(segment(cx, top, cx, bottom, 1.6), disc(cx, bottom+2.1, 0.95))
}

func gaugeShape(pct, pace float64, alert bool) shape {
	const cx, cy, rIn, rOut = 8.0, 8.0, 4.6, 6.8
	used := math.Min(math.Max(pct, 0), 100) / 100
	track := faded(ring(cx, cy, rIn, rOut), 0.3)
	fill := empty
	if used > 0 {
		fill = arc(cx, cy, rIn, rOut, 0, used)
	}
	layers := []shape{track, fill}
	if pace >= 0 && pace <= 1 {
		x1, y1 := pointOnCircle(cx, cy, rIn-0.3, pace)
		x2, y2 := pointOnCircle(cx, cy, rOut+0.3, pace)
		marker := segment(x1, y1, x2, y2, 1.2)
		if used >= pace {
			layers = []shape{minus(union(track, fill), marker)}
		} else {
			layers = append(layers, marker)
		}
	}
	if alert {
		layers = append(layers, exclamation(cx, 5.2, 8.3))
	}
	return union(layers...)
}

func gaugeGlyph(pct, pace float64, alert bool) glyph {
	return makeGlyph(gaugeShape(pct, pace, alert))
}

func unknownGlyph() glyph {
	return makeGlyph(union(faded(ring(8, 8, 4.6, 6.8), 0.3), disc(8, 8, 1.1)))
}

func refreshShape() shape {
	const cx, cy, rIn, rOut = 8.0, 8.6, 4.3, 5.9
	const end = 0.86
	body := arc(cx, cy, rIn, rOut, 0.04, end)
	mx, my := pointOnCircle(cx, cy, (rIn+rOut)/2, end)
	a := end * 2 * math.Pi
	tx, ty := math.Cos(a), math.Sin(a)
	nx, ny := math.Sin(a), -math.Cos(a)
	head := triangle(
		mx+tx*3.0, my+ty*3.0,
		mx+nx*2.7-tx*0.4, my+ny*2.7-ty*0.4,
		mx-nx*2.7-tx*0.4, my-ny*2.7-ty*0.4,
	)
	return union(body, head)
}

func gearShape() shape {
	body := func(x, y float64) float64 {
		dx, dy := x-8, y-8
		r := math.Hypot(dx, dy)
		if r <= 5.1 {
			return 1
		}
		return inside(r <= 7.1 && math.Cos(8*math.Atan2(dy, dx)) > 0.25)
	}
	return minus(body, disc(8, 8, 2.3))
}

func powerShape() shape {
	return union(arc(8, 8.8, 4.7, 6.3, 0.11, 0.89), segment(8, 1.8, 8, 7.6, 1.6))
}

func warningShape() shape {
	return minus(triangle(8, 1.2, 15.2, 14.6, 0.8, 14.6), exclamation(8, 6.0, 9.6))
}

func checkShape() shape {
	return minus(disc(8, 8, 7), union(segment(4.7, 8.3, 7.0, 10.6, 1.7), segment(7.0, 10.6, 11.4, 5.9, 1.7)))
}

type trayGlyphs struct {
	refresh, prefs, quit, warning, ok, spacer, unknown glyph
}

func newTrayGlyphs() trayGlyphs {
	return trayGlyphs{
		refresh: makeGlyph(refreshShape()),
		prefs:   makeGlyph(gearShape()),
		quit:    makeGlyph(powerShape()),
		warning: makeGlyph(warningShape()),
		ok:      makeGlyph(checkShape()),
		spacer:  makeGlyph(empty),
		unknown: unknownGlyph(),
	}
}

var logoCache = map[string]glyph{}

func logoGlyph(provider string) glyph {
	if g, ok := logoCache[provider]; ok {
		return g
	}
	g := makeGlyph(empty)
	if paths, ok := providerLogoPaths[provider]; ok {
		if s, err := logoShape(paths); err == nil {
			g = makeGlyph(s)
		}
	}
	logoCache[provider] = g
	return g
}

var alertLogoCache = map[string]glyph{}

func alertLogoGlyph(provider string) glyph {
	if g, ok := alertLogoCache[provider]; ok {
		return g
	}
	const bx, by = 12.2, 12.2
	badge := minus(disc(bx, by, 3.8), union(segment(bx, by-2.1, bx, by+0.2, 1.2), disc(bx, by+1.9, 0.7)))
	logo := empty
	if paths, ok := providerLogoPaths[provider]; ok {
		if s, err := logoShape(paths); err == nil {
			logo = s
		}
	}
	g := makeGlyph(union(minus(logo, disc(bx, by, 5.0)), badge))
	alertLogoCache[provider] = g
	return g
}
