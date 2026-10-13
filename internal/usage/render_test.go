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

func TestRemainingBarFullThenEmpty(t *testing.T) {
	full := remainingBar(12.34, "USD", 8)
	if !strings.Contains(full, "$12.34") {
		t.Fatalf("remainingBar(12.34) = %q, want amount", full)
	}
	if !strings.Contains(full, "[████████]") {
		t.Fatalf("remainingBar(12.34) should be full: %q", full)
	}

	empty := remainingBar(0, "USD", 8)
	if !strings.Contains(empty, "[░░░░░░░░]") {
		t.Fatalf("remainingBar(0) should be empty: %q", empty)
	}
	if !strings.Contains(empty, "$0.00") {
		t.Fatalf("remainingBar(0) = %q, want amount", empty)
	}
}

func TestRenderWindowRemaining(t *testing.T) {
	remaining := 5.0
	win := Window{Label: "Balance", Remaining: &remaining, Currency: "USD"}
	out := renderWindow(win, time.Now(), 8)
	if !strings.Contains(out, "$5.00") {
		t.Fatalf("renderWindow remaining = %q, want amount", out)
	}
}

func TestFormatMoney(t *testing.T) {
	cases := map[string]string{
		"CNY": "¥1.50",
		"USD": "$1.50",
		"EUR": "€1.50",
		"GBP": "£1.50",
		"JPY": "1.50 JPY",
		"":    "1.50",
	}
	for currency, want := range cases {
		if got := FormatMoney(1.5, currency); got != want {
			t.Errorf("FormatMoney(1.5, %q) = %q, want %q", currency, got, want)
		}
	}
}
