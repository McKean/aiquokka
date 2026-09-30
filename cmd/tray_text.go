package cmd

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/McKean/aiquokka/internal/usage"
)

// textIndent sets the window rows apart from their provider header, since
// many Linux tray hosts draw separators too faintly to see.
const textIndent = "    "

// providerURLs are the usage pages a provider header opens when clicked.
var providerURLs = map[string]string{
	"Claude":   "https://claude.ai/settings/usage",
	"Codex":    "https://chatgpt.com/codex/settings/usage",
	"Copilot":  "https://github.com/settings/copilot",
	"DeepSeek": "https://platform.deepseek.com/usage",
	"Kimi":     "https://www.kimi.com/code/console",
	"Kiro":     "https://app.kiro.dev",
	"Z.ai":     "https://z.ai/subscribe",
}

func openURL(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, url)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// menuLabel escapes underscores, which DBusMenu hosts otherwise swallow as
// mnemonic markers ("default_claude_max_20x" shows as "defaultclaudemax20x").
func menuLabel(s string) string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return s
	}
	return strings.ReplaceAll(s, "_", "__")
}

var claudeTierSize = regexp.MustCompile(`_(\d+x)$`)

// displayPlan turns raw plan identifiers into something readable, e.g.
// Claude's "max/default_claude_max_20x" into "Max 20x" and "plus" into "Plus".
func displayPlan(provider, plan string) string {
	if provider == "Claude" {
		sub, tier, _ := strings.Cut(plan, "/")
		if sub == "" {
			sub = tier
		}
		if m := claudeTierSize.FindStringSubmatch(tier); m != nil {
			return capitalize(sub) + " " + m[1]
		}
		if !strings.Contains(sub, "_") {
			return capitalize(sub)
		}
		return plan
	}
	if plan == strings.ToLower(plan) {
		return capitalize(plan)
	}
	return plan
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

func providerHeader(provider, plan string) string {
	if plan = displayPlan(provider, plan); plan != "" {
		return fmt.Sprintf("%s  ·  %s", provider, plan)
	}
	return provider
}

// shortReset gives one reset time: the clock time within the next day,
// otherwise the weekday too.
func shortReset(t, now time.Time) string {
	if !t.After(now) {
		return "resets now"
	}
	local := t.Local()
	if t.Sub(now) < 24*time.Hour {
		return "resets " + local.Format("15:04")
	}
	return "resets " + local.Format("Mon 15:04")
}

func windowValue(w usage.Window) string {
	pct, hasPct := windowPercent(w)
	switch {
	case hasPct && w.UsedPercent == nil:
		return fmt.Sprintf("%s (%d/%d)", formatPct(pct), *w.Used, *w.Limit)
	case hasPct:
		return formatPct(pct)
	case w.Remaining != nil:
		return usage.FormatMoney(*w.Remaining, w.Currency)
	default:
		return "—"
	}
}

// textColumns holds the column widths shared by every window row in the
// menu, so values line up when the tray host uses a monospace font.
type textColumns struct {
	label, value int
}

func newTextColumns(windows []usage.Window) textColumns {
	var c textColumns
	for _, w := range windows {
		c.label = max(c.label, utf8.RuneCountInString(w.Label))
		c.value = max(c.value, utf8.RuneCountInString(windowValue(w)))
	}
	return c
}

func (c textColumns) row(w usage.Window, now time.Time) string {
	value := windowValue(w)
	s := textIndent + padRight(w.Label, c.label) + "   " + padLeft(value, c.value)
	if !w.ResetsAt.IsZero() {
		s += "   " + shortReset(w.ResetsAt, now)
	}
	return strings.TrimRight(s, " ")
}

func factRow(f usage.Fact) string {
	return fmt.Sprintf("%s%s: %s", textIndent, f.Label, f.Value)
}

func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-utf8.RuneCountInString(s)))
}

func padLeft(s string, width int) string {
	return strings.Repeat(" ", max(0, width-utf8.RuneCountInString(s))) + s
}
