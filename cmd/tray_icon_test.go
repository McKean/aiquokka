package cmd

import (
	"bytes"
	"image/png"
	"testing"
)

func TestGaugeGlyphFillsProportionally(t *testing.T) {
	coverage := func(pct float64) int {
		img, err := png.Decode(bytes.NewReader(gaugeGlyph(pct, -1, false).template))
		if err != nil {
			t.Fatal(err)
		}
		opaque := 0
		for y := 0; y < glyphPixels; y++ {
			for x := 0; x < glyphPixels; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0xE000 {
					opaque++
				}
			}
		}
		return opaque
	}
	empty, half, full := coverage(0), coverage(50), coverage(100)
	if empty != 0 || !(half > 0 && half < full) {
		t.Errorf("expected 0 < half < full opaque pixels, got empty=%d half=%d full=%d", empty, half, full)
	}
}

func TestTrayIconsGeneration(t *testing.T) {
	tmpl, reg := getTrayIcons()
	if len(tmpl) == 0 {
		t.Fatal("expected non-empty template icon")
	}
	if len(reg) == 0 {
		t.Fatal("expected non-empty regular icon")
	}

	imgTmpl, err := png.Decode(bytes.NewReader(tmpl))
	if err != nil {
		t.Fatalf("failed to decode template PNG: %v", err)
	}
	if imgTmpl.Bounds().Dx() != 32 || imgTmpl.Bounds().Dy() != 32 {
		t.Errorf("expected 32x32 template icon, got %dx%d", imgTmpl.Bounds().Dx(), imgTmpl.Bounds().Dy())
	}

	imgReg, err := png.Decode(bytes.NewReader(reg))
	if err != nil {
		t.Fatalf("failed to decode regular PNG: %v", err)
	}
	if imgReg.Bounds().Dx() != 32 || imgReg.Bounds().Dy() != 32 {
		t.Errorf("expected 32x32 regular icon, got %dx%d", imgReg.Bounds().Dx(), imgReg.Bounds().Dy())
	}

	for name, g := range map[string]glyph{
		"gauge":   gaugeGlyph(75, 0.5, false),
		"alert":   gaugeGlyph(95, -1, true),
		"refresh": newTrayGlyphs().refresh,
	} {
		for kind, data := range map[string][]byte{"template": g.template, "regular": g.regular} {
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s %s: failed to decode PNG: %v", name, kind, err)
			}
			if img.Bounds().Dx() != glyphPixels || img.Bounds().Dy() != glyphPixels {
				t.Errorf("%s %s: expected %dx%d, got %v", name, kind, glyphPixels, glyphPixels, img.Bounds())
			}
		}
	}
}
