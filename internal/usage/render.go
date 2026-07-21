package usage

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

// Render writes a human-readable view of a Report to w.
func Render(w io.Writer, r *Report, now time.Time) {
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
		fmt.Fprintln(w, renderWindow(win, now))
	}
	for _, f := range r.Extra {
		fmt.Fprintf(w, "  %-14s %s\n", f.Label+":", f.Value)
	}
}

func renderWindow(win Window, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %-8s ", win.Label)

	pace := win.Pace(now)
	if win.UsedPercent != nil {
		pct := *win.UsedPercent
		b.WriteString(bar(pct, pace, 24))
		fmt.Fprintf(&b, " %5.1f%%", pct)
	} else if win.Used != nil && win.Limit != nil && *win.Limit > 0 {
		pct := float64(*win.Used) / float64(*win.Limit) * 100
		b.WriteString(bar(pct, pace, 24))
		fmt.Fprintf(&b, " %5.1f%% (%d/%d)", pct, *win.Used, *win.Limit)
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
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorPace   = "\033[96m" // bright cyan — the on-track pace marker
)

// stripANSI removes color codes for length calculations.
func stripANSI(s string) string { return s }
