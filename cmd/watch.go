package cmd

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"golang.org/x/term"
)

// watchAction is how waitForRefresh ended.
type watchAction int

const (
	watchStop   watchAction = iota // context cancelled / quit key
	watchTick                       // interval elapsed
	watchManual                     // user pressed r
)

// pulseIntensity cycles SGR intensity for a soft heartbeat on the status icon.
// Each entry is applied immediately before the ● glyph (cyan is set outside).
var pulseIntensity = []string{
	"\033[2m", // dim
	"\033[2m",
	"",
	"\033[1m", // bold
	"\033[1m",
	"",
	"\033[2m",
}

const (
	watchStatusTick = 120 * time.Millisecond
	colorDim        = "\033[2m"
	colorCyan       = "\033[96m"
	colorReset      = "\033[0m"
)

// waitForRefresh blocks until the next refresh should run or the user quits.
// On an interactive TTY it draws a pulsating status line with a countdown and
// accepts r to refresh early; otherwise it just sleeps.
func waitForRefresh(ctx context.Context, interval time.Duration) watchAction {
	if structured() || !stdoutIsTTY() {
		return waitForRefreshPlain(ctx, interval)
	}
	if stdinIsTTY() {
		return waitForRefreshInteractive(ctx, interval)
	}
	return waitForRefreshStatus(ctx, interval, false)
}

func waitForRefreshPlain(ctx context.Context, interval time.Duration) watchAction {
	select {
	case <-ctx.Done():
		return watchStop
	case <-time.After(interval):
		return watchTick
	}
}

// waitForRefreshStatus draws the pulsating countdown without reading keys.
func waitForRefreshStatus(ctx context.Context, interval time.Duration, showKeyHint bool) watchAction {
	return runWatchStatusLoop(ctx, interval, showKeyHint, nil)
}

// waitForRefreshInteractive puts stdin in raw non-blocking mode so a single
// keypress is enough while a ticker drives the countdown animation. Terminal
// settings are restored before returning so the next render sees a cooked TTY.
func waitForRefreshInteractive(ctx context.Context, interval time.Duration) watchAction {
	restore, err := enterWatchInput()
	if err != nil {
		return waitForRefreshStatus(ctx, interval, false)
	}
	defer restore()

	return runWatchStatusLoop(ctx, interval, true, pollWatchKey)
}

// runWatchStatusLoop animates the status line until the interval elapses, the
// context is cancelled, or poll returns a quit/refresh key. poll may be nil.
func runWatchStatusLoop(ctx context.Context, interval time.Duration, showKeyHint bool, poll func() (byte, bool)) watchAction {
	deadline := time.Now().Add(interval)
	ticker := time.NewTicker(watchStatusTick)
	defer ticker.Stop()

	fmt.Fprint(os.Stdout, "\033[?25l") // hide cursor
	defer fmt.Fprint(os.Stdout, "\033[?25h")

	// Status sits on its own line beneath the usage view.
	// Use \r\n so this is correct even if the terminal is briefly raw.
	fmt.Fprint(os.Stdout, "\r\n")
	frame := 0
	writeWatchStatus(os.Stdout, frame, time.Until(deadline), showKeyHint)

	for {
		if time.Until(deadline) <= 0 {
			fmt.Fprint(os.Stdout, "\r\033[2K")
			return watchTick
		}
		select {
		case <-ctx.Done():
			fmt.Fprint(os.Stdout, "\r\033[2K")
			return watchStop
		case <-ticker.C:
			if poll != nil {
				for {
					b, ok := poll()
					if !ok {
						break
					}
					switch b {
					case 'r', 'R':
						fmt.Fprint(os.Stdout, "\r\033[2K")
						return watchManual
					case 3, 'q', 'Q': // Ctrl+C or q
						fmt.Fprint(os.Stdout, "\r\033[2K")
						return watchStop
					}
				}
			}
			frame++
			writeWatchStatus(os.Stdout, frame, time.Until(deadline), showKeyHint)
		}
	}
}

// writeWatchStatus rewrites the current line with the pulse icon and countdown.
func writeWatchStatus(w io.Writer, frame int, remaining time.Duration, showKeyHint bool) {
	intensity := pulseIntensity[frame%len(pulseIntensity)]
	icon := colorCyan + intensity + "●" + colorReset
	countdown := formatCountdown(remaining)
	var line string
	if showKeyHint {
		line = fmt.Sprintf("  %s  next refresh in %s  %s·%s  press %sr%s to refresh  %s·%s  %sq%s to close",
			icon,
			countdown,
			colorDim, colorReset,
			colorCyan, colorReset,
			colorDim, colorReset,
			colorCyan, colorReset,
		)
	} else {
		line = fmt.Sprintf("  %s  next refresh in %s", icon, countdown)
	}
	// \r + clear-line keeps the status on one row; flush so the countdown
	// advances even when stdout is fully buffered (some terminals/PTY setups).
	fmt.Fprintf(w, "\r\033[2K%s", line)
	if f, ok := w.(*os.File); ok {
		_ = f.Sync()
	}
}

// formatCountdown renders remaining time as m:ss (ceil so 60s shows as 1:00).
func formatCountdown(remaining time.Duration) string {
	if remaining < 0 {
		remaining = 0
	}
	sec := int(math.Ceil(remaining.Seconds()))
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

func stdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
