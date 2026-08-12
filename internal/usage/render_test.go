package usage

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestLabelWidthAcrossReports(t *testing.T) {
	short := &Report{Windows: []Window{{Label: "5h"}}}
	long := &Report{Windows: []Window{{Label: "Premium Interactions"}}}

	if got, want := LabelWidth(short, long), len("Premium Interactions"); got != want {
		t.Fatalf("LabelWidth() = %d, want %d", got, want)
	}
}

func TestLabelWidthHasMinimum(t *testing.T) {
	if got := LabelWidth(&Report{Windows: []Window{{Label: "5h"}}}); got != 8 {
		t.Fatalf("LabelWidth() = %d, want 8", got)
	}
}

func TestRenderAlignedUsesSharedBarColumn(t *testing.T) {
	used := 25.0
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	short := &Report{Provider: "Short", Windows: []Window{{Label: "5h", UsedPercent: &used}}}
	long := &Report{Provider: "Long", Windows: []Window{{Label: "Weekly Fable", UsedPercent: &used}}}
	width := LabelWidth(short, long)

	var shortOut, longOut bytes.Buffer
	RenderAligned(&shortOut, short, now, width)
	RenderAligned(&longOut, long, now, width)

	shortBar := strings.Index(shortOut.String(), "\033[32m[")
	longBar := strings.Index(longOut.String(), "\033[32m[")
	if shortBar < 0 || longBar < 0 {
		t.Fatalf("bar missing from output:\n%s\n%s", shortOut.String(), longOut.String())
	}
	shortLine := strings.LastIndex(shortOut.String()[:shortBar], "\n")
	longLine := strings.LastIndex(longOut.String()[:longBar], "\n")
	if got, want := shortBar-shortLine, longBar-longLine; got != want {
		t.Fatalf("bar columns differ: short=%d long=%d", got, want)
	}
}
