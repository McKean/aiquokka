// Package usage defines the common data model that every provider reports.
package usage

import "time"

// Window is a single rate-limit window (e.g. a 5-hour rolling window or a
// weekly window) as reported by a provider.
type Window struct {
	// Label is a human name for the window, e.g. "5h" or "Weekly".
	Label string `json:"label" yaml:"label"`
	// UsedPercent is how much of the window has been consumed, 0-100.
	// It is nil when the provider does not report a percentage.
	UsedPercent *float64 `json:"used_percent,omitempty" yaml:"used_percent,omitempty"`
	// ResetsAt is when this window's allowance next resets. Zero if unknown.
	ResetsAt time.Time `json:"resets_at,omitempty" yaml:"resets_at,omitempty"`
	// Used and Limit are absolute counts when the provider reports them.
	// Both nil when only a percentage is available.
	Used  *int64 `json:"used,omitempty" yaml:"used,omitempty"`
	Limit *int64 `json:"limit,omitempty" yaml:"limit,omitempty"`
	// Remaining is the amount still available for a prepaid, balance-style
	// allowance where the provider reports an absolute balance but no starting
	// amount or limit (e.g. DeepSeek's account balance). When set — and
	// UsedPercent/Used/Limit are not — the renderer draws a full "remaining"
	// bar labelled with the amount; spending it runs the bar down toward zero.
	Remaining *float64 `json:"remaining,omitempty" yaml:"remaining,omitempty"`
	// Currency is the ISO currency code for Remaining (e.g. "CNY", "USD").
	Currency string `json:"currency,omitempty" yaml:"currency,omitempty"`
	// Duration is the full length of the window (e.g. 5h, 7d). When set along
	// with ResetsAt it lets the renderer draw a linear-pace marker showing where
	// even consumption would put you at the current time.
	Duration time.Duration `json:"-" yaml:"-"`
}

// Pace returns the fraction (0-1) of the window that has elapsed at now — the
// point at which usage would sit if consumed evenly. It returns -1 when the
// window's timing is unknown.
func (w Window) Pace(now time.Time) float64 {
	if w.Duration <= 0 || w.ResetsAt.IsZero() {
		return -1
	}
	start := w.ResetsAt.Add(-w.Duration)
	elapsed := now.Sub(start).Seconds()
	frac := elapsed / w.Duration.Seconds()
	if frac < 0 {
		return 0
	}
	if frac > 1 {
		return 1
	}
	return frac
}

// Report is a provider's full usage picture.
type Report struct {
	// Provider is the display name, e.g. "Claude".
	Provider string `json:"provider" yaml:"provider"`
	// Plan describes the subscription tier when known, e.g. "max_20x".
	Plan string `json:"plan,omitempty" yaml:"plan,omitempty"`
	// Windows are the rate-limit windows, in display order.
	Windows []Window `json:"windows" yaml:"windows"`
	// Extra holds provider-specific labelled facts (e.g. Codex reset counts)
	// rendered as-is beneath the windows.
	Extra []Fact `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// Fact is an arbitrary labelled value for provider-specific details.
type Fact struct {
	Label string `json:"label" yaml:"label"`
	Value string `json:"value" yaml:"value"`
}
