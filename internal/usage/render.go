package usage

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

// Render writes a human-readable view of a Report to w, aligning its bars to
// the longest window label in that report.
func Render(w io.Writer, r *Report, now time.Time) {
	RenderAligned(w, r, now, LabelWidth(r))
}

// LabelWidth returns the width needed to align window labels across reports.
// Eight characters is the minimum used by the standard single-provider view.
func LabelWidth(reports ...*Report) int {
	width := 8
	for _, report := range reports {
		if report == nil {
			continue
		}
		for _, win := range report.Windows {
			if len(win.Label) > width {
				width = len(win.Label)
			}
		}
	}
	return width
}

// RenderAligned writes a report using labelWidth for the window-label column.
// Aggregate callers can pass one shared width so bars line up across providers.
func RenderAligned(w io.Writer, r *Report, now time.Time, labelWidth int) {
	if labelWidth < 8 {
		labelWidth = 8
	}
	title := r.Provider
	if r.Plan != "" {
		title = fmt.Sprintf("%s  (%s)", r.Provider, r.Plan)
	}
	fmt.Fprintln(w, title)
	fmt.Fprintln(w, strings.Repeat("─", len(stripANSI(title))))

	if len(r.Windows) == 0 {
		fmt.Fprintln(w, "  no usage windows reported")
	}
	for _, win := range r.Windows {
		fmt.Fprintln(w, renderWindow(win, now, labelWidth))
	}
	for _, f := range r.Extra {
		fmt.Fprintf(w, "  %-14s %s\n", f.Label+":", f.Value)
	}
}

func renderWindow(win Window, now time.Time, maxLabel int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %-*s ", maxLabel, win.Label)

	pace := win.Pace(now)
	if win.UsedPercent != nil {
		pct := *win.UsedPercent
		b.WriteString(bar(pct, pace, 24))
		fmt.Fprintf(&b, " %5.1f%%", pct)
	} else if win.Used != nil && win.Limit != nil && *win.Limit > 0 {
		pct := float64(*win.Used) / float64(*win.Limit) * 100
		b.WriteString(bar(pct, pace, 24))
		fmt.Fprintf(&b, " %5.1f%% (%d/%d)", pct, *win.Used, *win.Limit)
	} else if win.Remaining != nil {
		b.WriteString(remainingBar(*win.Remaining, win.Currency, 24))
	} else {
		b.WriteString(strings.Repeat(" ", 24) + "     ?")
	}

	if !win.ResetsAt.IsZero() {
		fmt.Fprintf(&b, "   resets %s", humanizeReset(win.ResetsAt, now))
	}
	return b.String()
}

// bar renders a colored progress bar for pct in [0,100]. When pace is in [0,1]
// it draws a marker cell at the linear-pace position — where even consumption
// over the window would put you right now — so you can see at a glance whether
// you are ahead of or behind schedule.
func bar(pct, pace float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(math.Round(pct / 100 * float64(width)))
	color := colorGreen
	switch {
	case pct >= 90:
		color = colorRed
	case pct >= 70:
		color = colorYellow
	}

	markerIdx := -1
	if pace >= 0 {
		markerIdx = int(math.Round(pace * float64(width)))
		if markerIdx >= width {
			markerIdx = width - 1
		}
	}

	var cells strings.Builder
	for i := 0; i < width; i++ {
		if i == markerIdx {
			// The pace marker: a bright cell marking the on-track position.
			glyph := "▓"
			if i >= filled {
				glyph = "▒"
			}
			fmt.Fprintf(&cells, "%s%s%s", colorPace, glyph, color)
			continue
		}
		if i < filled {
			cells.WriteString("█")
		} else {
			cells.WriteString("░")
		}
	}
	return fmt.Sprintf("%s[%s]%s", color, cells.String(), colorReset)
}

// remainingBar draws a bar for a prepaid balance. The starting amount is
// unknown, so the bar is full whenever any balance remains and empties at
// zero: the amount printed next to it is the source of truth.
func remainingBar(amount float64, currency string, width int) string {
	filled := width
	if amount <= 0 {
		filled = 0
	}
	color := colorRemaining
	if amount <= 0 {
		color = colorRed
	}
	var cells strings.Builder
	for i := 0; i < filled; i++ {
		cells.WriteString("█")
	}
	for i := filled; i < width; i++ {
		cells.WriteString("░")
	}
	return fmt.Sprintf("%s[%s]%s %s", color, cells.String(), colorReset, FormatMoney(amount, currency))
}

// FormatMoney renders an amount with a familiar symbol for common currencies.
func FormatMoney(amount float64, currency string) string {
	switch strings.ToUpper(currency) {
	case "CNY":
		return fmt.Sprintf("¥%.2f", amount)
	case "USD":
		return fmt.Sprintf("$%.2f", amount)
	case "EUR":
		return fmt.Sprintf("€%.2f", amount)
	case "GBP":
		return fmt.Sprintf("£%.2f", amount)
	default:
		if currency == "" {
			return fmt.Sprintf("%.2f", amount)
		}
		return fmt.Sprintf("%.2f %s", amount, currency)
	}
}

// humanizeReset formats a reset time relative to now, e.g. "in 3h12m (18:40)".
func humanizeReset(t, now time.Time) string {
	d := t.Sub(now)
	if d <= 0 {
		return "now"
	}
	local := t.Local()
	var rel string
	switch {
	case d >= 24*time.Hour:
		days := int(d.Hours()) / 24
		hrs := int(d.Hours()) % 24
		rel = fmt.Sprintf("in %dd%dh", days, hrs)
	case d >= time.Hour:
		rel = fmt.Sprintf("in %dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		rel = fmt.Sprintf("in %dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%s (%s)", rel, local.Format("Mon 15:04"))
}

const (
	colorReset     = "\033[0m"
	colorRed       = "\033[31m"
	colorYellow    = "\033[33m"
	colorGreen     = "\033[32m"
	colorPace      = "\033[96m" // bright cyan — the on-track pace marker
	colorRemaining = "\033[92m" // bright green — a remaining prepaid balance
)

// stripANSI removes color codes for length calculations.
func stripANSI(s string) string { return s }
