package kimi

import (
	"math"
	"testing"
)

func TestToWindowSetsPercentageAndKeepsCounts(t *testing.T) {
	w := toWindow("Weekly", row{
		Used:  num{set: true, v: 78},
		Limit: num{set: true, v: 100},
	})
	if w == nil {
		t.Fatal("toWindow() returned nil")
	}
	if w.Used == nil || *w.Used != 78 || w.Limit == nil || *w.Limit != 100 {
		t.Fatalf("absolute counts not preserved: %#v", w)
	}
	if w.UsedPercent == nil || math.Abs(*w.UsedPercent-78) > 0.000001 {
		t.Fatalf("UsedPercent = %v, want 78", w.UsedPercent)
	}
}

func TestToWindowDerivesPercentageFromRemaining(t *testing.T) {
	w := toWindow("5h", row{
		Limit:     num{set: true, v: 100},
		Remaining: num{set: true, v: 85},
	})
	if w == nil || w.Used == nil || *w.Used != 15 {
		t.Fatalf("derived usage = %#v, want 15", w)
	}
	if w.UsedPercent == nil || math.Abs(*w.UsedPercent-15) > 0.000001 {
		t.Fatalf("UsedPercent = %v, want 15", w.UsedPercent)
	}
}
