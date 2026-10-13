package cmd

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

func TestParseSVGPathEvenOdd(t *testing.T) {
	sub, err := parseSVGPath("M0 0H24V24H0z M6 6H18V18H6z")
	if err != nil {
		t.Fatal(err)
	}
	s := evenOddShape(sub, 24, 24, 0)
	if s(2, 2) != 1 || s(12, 12) != 0 || s(25, 2) != 0 {
		t.Errorf("even-odd fill wrong: corner=%v hole=%v outside=%v", s(2, 2), s(12, 12), s(25, 2))
	}
}

func TestParseSVGPathArcsAndRelative(t *testing.T) {
	sub, err := parseSVGPath("M0 12a12 12 0 1 0 24 0a12 12 0 1 0-24 0z")
	if err != nil {
		t.Fatal(err)
	}
	circle := evenOddShape(sub, 24, 24, 0)
	if circle(12, 12) != 1 || circle(1, 1) != 0 || circle(12, 23) != 1 {
		t.Errorf("arc circle wrong")
	}

	sub, err = parseSVGPath("m2 2 4 0 0 4z")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 1 || len(sub[0]) != 4 || sub[0][2] != (point{6, 6}) {
		t.Errorf("implicit relative lineto wrong: %v", sub)
	}

	sub, err = parseSVGPath("M1 1a1 1 0 011 1")
	if err != nil || len(sub) != 1 {
		t.Errorf("compact arc flags not parsed: %v %v", sub, err)
	}
}

func opaquePixels(t *testing.T, data []byte) (int, image.Image) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0x8000 {
				n++
			}
		}
	}
	return n, img
}

func TestEveryProviderHasALogo(t *testing.T) {
	for _, p := range allProviders {
		paths, ok := providerLogoPaths[p.name]
		if !ok {
			t.Errorf("no logo for provider %q", p.name)
			continue
		}
		if _, err := logoShape(paths); err != nil {
			t.Errorf("logo for %q does not parse: %v", p.name, err)
			continue
		}
		if n, _ := opaquePixels(t, logoGlyph(p.name).template); n < 40 {
			t.Errorf("logo for %q renders almost empty (%d px)", p.name, n)
		}
	}
}

func TestNewUsageRow(t *testing.T) {
	now := time.Now()
	win := pctWindow("Weekly", 88, now.Add(3*time.Hour))
	win.Duration = 7 * 24 * time.Hour
	r := newUsageRow(win, now, 80)
	if r.value != "88%" || r.fill != 0.88 || !r.alert || r.pace < 0 || r.reset == "" {
		t.Errorf("unexpected row: %+v", r)
	}
	if contains(r.reset, "(") {
		t.Errorf("reset should be compact: %q", r.reset)
	}

	used, limit := int64(3), int64(50)
	r = newUsageRow(usage.Window{Label: "Credits", Used: &used, Limit: &limit}, now, 80)
	if r.value != "6%" || r.alert || !contains(r.tooltip, "3/50 used") {
		t.Errorf("unexpected used/limit row: %+v", r)
	}

	zero := 0.0
	r = newUsageRow(usage.Window{Label: "Balance", Remaining: &zero, Currency: "USD"}, now, 80)
	if !r.alert || r.fill != 0 || r.pace != -1 {
		t.Errorf("empty balance should alert: %+v", r)
	}

	r = newUsageRow(usage.Window{Label: "Spend"}, now, 80)
	if r.value != "—" || r.fill != -1 {
		t.Errorf("unknown row: %+v", r)
	}
}

func TestTableRowsAlignRegardlessOfLabel(t *testing.T) {
	face := loadRowFace()
	if face == nil {
		t.Skip("system font not available")
	}
	now := time.Now()
	short := newUsageRow(pctWindow("5h", 50, now.Add(time.Hour)), now, 80)
	long := newUsageRow(pctWindow("Weekly Fable", 50, now.Add(90*time.Hour)), now, 80)
	layout := newTableLayout(face, []usageRow{short, long}, nil)
	warn := newTrayGlyphs().warning.template

	_, a := opaquePixels(t, renderUsageRow(layout, short, warn))
	_, b := opaquePixels(t, renderUsageRow(layout, long, warn))
	if a.Bounds() != b.Bounds() {
		t.Fatalf("rows differ in size: %v vs %v", a.Bounds(), b.Bounds())
	}

	barStart := func(img image.Image) int {
		y := img.Bounds().Dy() / 2
		from := int(rowIndent * rowScale)
		for x := from; x < img.Bounds().Dx(); x++ {
			if _, _, _, al := img.At(x, y).RGBA(); al > 0x8000 {
				if x > int(layout.barX()*rowScale)-4 {
					return x
				}
			}
		}
		return -1
	}
	if sa, sb := barStart(a), barStart(b); sa < 0 || sa != sb {
		t.Errorf("bars start at different x: %d vs %d", sa, sb)
	}
}

func TestLongLabelsAreTruncated(t *testing.T) {
	face := loadRowFace()
	if face == nil {
		t.Skip("system font not available")
	}
	row := usageRow{label: "An extremely long window label that never ends", value: "1%", fill: 0.01, pace: -1}
	layout := newTableLayout(face, []usageRow{row}, nil)
	if layout.labelW > rowMaxLabel {
		t.Errorf("label column not capped: %v", layout.labelW)
	}
	if got := layout.truncate(row.label); !contains(got, "…") || layout.measure(got) > layout.labelW {
		t.Errorf("label not truncated to column: %q", got)
	}
}

func BenchmarkRenderMenuRows(b *testing.B) {
	face := loadRowFace()
	if face == nil {
		b.Skip("system font not available")
	}
	now := time.Now()
	rows := []usageRow{
		newUsageRow(pctWindow("5h", 12, now.Add(time.Hour)), now, 80),
		newUsageRow(pctWindow("Weekly", 44, now.Add(50*time.Hour)), now, 80),
		newUsageRow(pctWindow("Weekly Fable", 91, now.Add(50*time.Hour)), now, 80),
	}
	layout := newTableLayout(face, rows, nil)
	warn := newTrayGlyphs().warning.template
	logo := logoGlyph("Claude").template
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 9; j++ {
			renderHeaderRow(face, logo, "Claude", "max_5x", 0)
			for _, r := range rows {
				renderUsageRow(layout, r, warn)
			}
		}
	}
}
