package cmd

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	rowScale       = 2.0
	rowHeight      = 20.0
	rowFontSize    = 13.0
	rowIndent      = 22.0
	rowLogoSize    = 16.0
	rowMaxLabel    = 130.0
	rowColumnGap   = 12.0
	rowBarWidth    = 132.0
	rowBarHeight   = 7.0
	rowAlertWidth  = 14.0
	rowTailPadding = 6.0
	secondaryAlpha = 0.5
	trackAlpha     = 0.2
)

var systemFontPaths = []string{
	"/System/Library/Fonts/SFNS.ttf",
	"/System/Library/Fonts/Helvetica.ttc",
}

func loadRowFace() font.Face {
	for _, p := range systemFontPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f *opentype.Font
		if strings.HasSuffix(p, ".ttc") {
			coll, err := opentype.ParseCollection(data)
			if err != nil || coll.NumFonts() == 0 {
				continue
			}
			f, err = coll.Font(0)
			if err != nil {
				continue
			}
		} else if f, err = opentype.Parse(data); err != nil {
			continue
		}
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: rowFontSize, DPI: 72 * rowScale, Hinting: font.HintingNone})
		if err == nil {
			return face
		}
	}
	return nil
}

type usageRow struct {
	label   string
	value   string
	fill    float64
	pace    float64
	alert   bool
	reset   string
	tooltip string
}

func compactReset(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	return strings.NewReplacer(" (", " · ", ")", "").Replace(usage.HumanizeReset(t, now))
}

func newUsageRow(w usage.Window, now time.Time, threshold int) usageRow {
	r := usageRow{label: w.Label, fill: -1, pace: w.Pace(now), reset: compactReset(w.ResetsAt, now)}
	tips := []string{}
	pct, hasPct := windowPercent(w)
	switch {
	case hasPct:
		r.fill = math.Min(math.Max(pct, 0), 100) / 100
		r.value = formatPct(pct)
		r.alert = levelFor(pct, threshold) >= levelCritical
		if w.UsedPercent == nil {
			tips = append(tips, formatUsedLimit(*w.Used, *w.Limit))
		}
	case w.Remaining != nil:
		r.value = usage.FormatMoney(*w.Remaining, w.Currency)
		r.fill, r.alert = 1, *w.Remaining <= 0
		if r.alert {
			r.fill = 0
		}
		r.pace = -1
	default:
		r.value = "—"
		r.pace = -1
	}
	if t := windowTooltip(w, now); t != "" {
		tips = append(tips, t)
	}
	r.tooltip = strings.Join(tips, " · ")
	return r
}

func formatUsedLimit(used, limit int64) string {
	return fmt.Sprintf("%d/%d used", used, limit)
}

type tableLayout struct {
	face   font.Face
	labelW float64
	valueW float64
	resetW float64
}

func (t tableLayout) measure(s string) float64 {
	return float64(font.MeasureString(t.face, s)) / 64 / rowScale
}

func newTableLayout(face font.Face, rows []usageRow, facts []usage.Fact) tableLayout {
	t := tableLayout{face: face}
	for _, r := range rows {
		t.labelW = math.Max(t.labelW, t.measure(r.label))
		t.valueW = math.Max(t.valueW, t.measure(r.value))
		t.resetW = math.Max(t.resetW, t.measure(r.reset))
	}
	for _, f := range facts {
		t.labelW = math.Max(t.labelW, t.measure(f.Label))
	}
	t.labelW = math.Min(t.labelW, rowMaxLabel)
	t.valueW = math.Max(t.valueW, t.measure("100%"))
	return t
}

func (t tableLayout) barX() float64   { return rowIndent + t.labelW + rowColumnGap }
func (t tableLayout) valueR() float64 { return t.barX() + rowBarWidth + rowColumnGap + t.valueW }
func (t tableLayout) alertX() float64 { return t.valueR() + 4 }
func (t tableLayout) resetX() float64 { return t.alertX() + rowAlertWidth + 4 }
func (t tableLayout) width() float64 {
	return t.resetX() + t.resetW + rowTailPadding
}

type rowCanvas struct {
	img  *image.RGBA
	face font.Face
}

func newRowCanvas(face font.Face, width float64) *rowCanvas {
	w := int(math.Ceil(width * rowScale))
	h := int(math.Ceil(rowHeight * rowScale))
	return &rowCanvas{img: image.NewRGBA(image.Rect(0, 0, w, h)), face: face}
}

func (c *rowCanvas) png() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, c.img)
	return buf.Bytes()
}

func (c *rowCanvas) baseline() fixed.Int26_6 {
	m := c.face.Metrics()
	ascent := float64(m.Ascent) / 64
	descent := float64(m.Descent) / 64
	return fixed.Int26_6(math.Round(((rowHeight*rowScale-(ascent+descent))/2 + ascent) * 64))
}

func (c *rowCanvas) text(x float64, s string, alpha float64, bold bool) float64 {
	src := image.NewUniform(color.NRGBA{A: uint8(math.Round(255 * alpha))})
	d := &font.Drawer{Dst: c.img, Src: src, Face: c.face}
	start := fixed.Int26_6(math.Round(x * rowScale * 64))
	d.Dot = fixed.Point26_6{X: start, Y: c.baseline()}
	d.DrawString(s)
	if bold {
		d.Dot = fixed.Point26_6{X: start + 40, Y: c.baseline()}
		d.DrawString(s)
	}
	return float64(d.Dot.X-start) / 64 / rowScale
}

func (c *rowCanvas) textRight(right float64, s string, alpha float64, bold bool) {
	w := float64(font.MeasureString(c.face, s)) / 64 / rowScale
	c.text(right-w, s, alpha, bold)
}

func (c *rowCanvas) paint(s shape, x0, y0, x1, y1 float64) {
	b := c.img.Bounds()
	px0 := int(math.Max(0, math.Floor(x0*rowScale)))
	py0 := int(math.Max(0, math.Floor(y0*rowScale)))
	px1 := int(math.Min(float64(b.Dx()), math.Ceil(x1*rowScale)))
	py1 := int(math.Min(float64(b.Dy()), math.Ceil(y1*rowScale)))
	step := 1.0 / glyphSubsamples
	for py := py0; py < py1; py++ {
		for px := px0; px < px1; px++ {
			var sum float64
			for sy := 0; sy < glyphSubsamples; sy++ {
				for sx := 0; sx < glyphSubsamples; sx++ {
					x := (float64(px) + (float64(sx)+0.5)*step) / rowScale
					y := (float64(py) + (float64(sy)+0.5)*step) / rowScale
					sum += s(x, y)
				}
			}
			a := math.Min(sum/(glyphSubsamples*glyphSubsamples), 1)
			if a <= 0 {
				continue
			}
			if cur := float64(c.img.RGBAAt(px, py).A) / 255; cur > a {
				a = cur
			}
			c.img.SetRGBA(px, py, color.RGBA{A: uint8(math.Round(255 * a))})
		}
	}
}

func (c *rowCanvas) glyphAt(g []byte, x, y float64) {
	src, err := png.Decode(bytes.NewReader(g))
	if err != nil {
		return
	}
	at := image.Pt(int(math.Round(x*rowScale)), int(math.Round(y*rowScale)))
	draw.Draw(c.img, src.Bounds().Add(at), src, image.Point{}, draw.Over)
}

func roundedRect(x0, y0, x1, y1, r float64) shape {
	return func(x, y float64) float64 {
		if x < x0 || x > x1 || y < y0 || y > y1 {
			return 0
		}
		cx := math.Max(x0+r, math.Min(x, x1-r))
		cy := math.Max(y0+r, math.Min(y, y1-r))
		return inside(math.Hypot(x-cx, y-cy) <= r)
	}
}

func barShape(x0, fill, pace float64) shape {
	y0 := (rowHeight - rowBarHeight) / 2
	y1 := y0 + rowBarHeight
	x1 := x0 + rowBarWidth
	r := rowBarHeight / 2
	track := roundedRect(x0, y0, x1, y1, r)
	filled := roundedRect(x0, y0, x0+math.Max(fill*rowBarWidth, rowBarHeight), y1, r)
	if fill <= 0 {
		filled = empty
	}
	tickX := x0 + pace*rowBarWidth
	const tickHalf, gap = 0.75, 1.0
	return func(x, y float64) float64 {
		if pace >= 0 && y >= y0-2.5 && y <= y1+2.5 {
			d := math.Abs(x - tickX)
			if d <= tickHalf {
				return 1
			}
			if d <= tickHalf+gap && y >= y0 && y <= y1 {
				return 0
			}
		}
		if filled(x, y) > 0 {
			return 1
		}
		return track(x, y) * trackAlpha
	}
}

func (t tableLayout) truncate(s string) string {
	if t.measure(s) <= t.labelW {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		if cand := string(runes) + "…"; t.measure(cand) <= t.labelW {
			return cand
		}
	}
	return s
}

func renderUsageRow(t tableLayout, r usageRow, warning []byte) []byte {
	c := newRowCanvas(t.face, t.width())
	c.text(rowIndent, t.truncate(r.label), 1, false)
	fill := r.fill
	if fill < 0 {
		fill = 0
	}
	c.paint(barShape(t.barX(), fill, r.pace), t.barX()-3, 0, t.barX()+rowBarWidth+3, rowHeight)
	c.textRight(t.valueR(), r.value, 1, r.alert)
	if r.alert {
		c.glyphAt(warning, t.alertX()-1, (rowHeight-16)/2)
	}
	if r.reset != "" {
		c.text(t.resetX(), r.reset, secondaryAlpha, false)
	}
	return c.png()
}

func renderFactRow(t tableLayout, f usage.Fact) []byte {
	c := newRowCanvas(t.face, t.width())
	c.text(rowIndent, t.truncate(f.Label), secondaryAlpha, false)
	c.text(t.barX(), f.Value, secondaryAlpha, false)
	return c.png()
}

func renderHeaderRow(face font.Face, logo []byte, name, plan string, minWidth float64) []byte {
	probe := tableLayout{face: face}
	width := rowIndent + probe.measure(name) + 1
	if plan != "" {
		width += 8 + probe.measure(plan)
	}
	width = math.Max(width+rowTailPadding, minWidth)
	c := newRowCanvas(face, width)
	if logo != nil {
		c.glyphAt(logo, 0, (rowHeight-rowLogoSize)/2)
	}
	x := rowIndent + c.text(rowIndent, name, 1, true)
	if plan != "" {
		c.text(x+8, plan, secondaryAlpha, false)
	}
	return c.png()
}
