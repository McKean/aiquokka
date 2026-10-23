// Package usage defines the common data model that every provider reports.
package usage

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Window is a single rate-limit window (e.g. a 5-hour rolling window or a
// weekly window) as reported by a provider.
type Window struct {
	// ID names the window for programs: it is unique within a provider's
	// report and does not change when the display label is reworded. It comes
	// from the provider's own name for the limit where it has one, and from
	// the window's length otherwise (see LengthID).
	ID string `json:"id" yaml:"id"`
	// Label is a human name for the window, e.g. "5h" or "Weekly".
	Label string `json:"label" yaml:"label"`
	// UsedPercent is how much of the window has been consumed, 0-100.
	// It is nil when the provider does not report a percentage.
	UsedPercent *float64 `json:"used_percent,omitempty" yaml:"used_percent,omitempty"`
	// ResetsAt is when this window's allowance next resets. Zero if unknown.
	// omitzero, not omitempty: encoding/json never treats a struct as empty,
	// so omitempty printed an unknown reset as year 1.
	ResetsAt time.Time `json:"resets_at,omitzero" yaml:"resets_at,omitempty"`
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
	// Model is the model the window counts for, in the provider's own words
	// (e.g. "Fable"). Empty for a window every model draws from.
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// Duration is the full length of the window (e.g. 5h, 7d). When set along
	// with ResetsAt it lets the renderer draw a linear-pace marker showing where
	// even consumption would put you at the current time. Structured output
	// carries it as duration_seconds (see windowOut).
	Duration time.Duration `json:"-" yaml:"-"`
}

// windowOut is a Window as structured output prints it: every field under its
// own tag, plus the length in whole seconds and the share of the window that
// has passed, each left out when unknown.
type windowOut struct {
	plainWindow     `yaml:",inline"`
	DurationSeconds int64    `json:"duration_seconds,omitempty" yaml:"duration_seconds,omitempty"`
	TargetPercent   *float64 `json:"target_percent,omitempty" yaml:"target_percent,omitempty"`
}

// plainWindow has Window's fields without its marshalers, so encoding a
// windowOut does not recurse.
type plainWindow Window

func (w Window) out() windowOut {
	out := windowOut{plainWindow: plainWindow(w), DurationSeconds: int64(w.Duration / time.Second)}
	if target, ok := w.TargetPercent(time.Now()); ok {
		out.TargetPercent = &target
	}
	return out
}

func (w Window) MarshalJSON() ([]byte, error) { return json.Marshal(w.out()) }

func (w Window) MarshalYAML() (any, error) { return w.out(), nil }

// LengthID names a window by its length alone ("5h", "7d", "90m"), for
// providers that tell windows apart by nothing else. It is empty when the
// length is unknown.
func LengthID(d time.Duration) string {
	secs := int64(d / time.Second)
	switch {
	case secs <= 0:
		return ""
	case secs%86400 == 0:
		return strconv.FormatInt(secs/86400, 10) + "d"
	case secs%3600 == 0:
		return strconv.FormatInt(secs/3600, 10) + "h"
	case secs%60 == 0:
		return strconv.FormatInt(secs/60, 10) + "m"
	}
	return strconv.FormatInt(secs, 10) + "s"
}

// Slug turns a provider's display name into an ID part: lower case, with each
// run of characters other than letters, digits and dots replaced by one dash
// ("Gemini 3 Pro" -> "gemini-3-pro", "glm-5.3" stays).
func Slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	return b.String()
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

// TargetFloor and TargetCeiling bound the reported target: a window fresh
// from its reset reports 10, not 0, so consumers comparing usage against the
// target act from the first hour; a window at its end reports 90, leaving a
// buffer.
const (
	TargetFloor   = 10.0
	TargetCeiling = 90.0
)

// TargetPercent returns the share of the window that has passed at now,
// cushioned by a straight line from +10 at the reset to -10 at the end:
// TargetFloor at the reset, TargetCeiling at the end, the pace between. It
// reports false when either timing input is missing, so callers leave the key
// out instead of guessing.
func (w Window) TargetPercent(now time.Time) (float64, bool) {
	pace := w.Pace(now)
	if pace < 0 {
		return 0, false
	}
	return TargetFloor + (TargetCeiling-TargetFloor)*pace, true
}

// Report is a provider's full usage picture.
type Report struct {
	// Provider is the display name, e.g. "Claude".
	Provider string `json:"provider" yaml:"provider"`
	// Plan describes the subscription tier when known, e.g. "max_20x".
	Plan string `json:"plan,omitempty" yaml:"plan,omitempty"`
	// Account is whose login the report was fetched with, when the provider's
	// stored login names it. Nil for providers whose login does not.
	Account *Account `json:"account,omitempty" yaml:"account,omitempty"`
	// Windows are the rate-limit windows, in display order.
	Windows []Window `json:"windows" yaml:"windows"`
	// Extra holds provider-specific labelled facts (e.g. Codex reset counts)
	// rendered as-is beneath the windows.
	Extra []Fact `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// Account identifies a login without any secret from it. Either field may be
// empty: a provider fills what its stored login holds.
type Account struct {
	// ID is the provider's own identifier for the account: a UUID for Claude,
	// Codex and Grok, the GitHub login for Copilot.
	ID string `json:"id,omitempty" yaml:"id,omitempty"`
	// Email is the address the account is signed in with.
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
}

// NewAccount returns nil when the login named neither, so that a report
// leaves the key out instead of printing an empty object.
func NewAccount(id, email string) *Account {
	if id == "" && email == "" {
		return nil
	}
	return &Account{ID: id, Email: email}
}

// Fact is an arbitrary labelled value for provider-specific details.
type Fact struct {
	Label string `json:"label" yaml:"label"`
	Value string `json:"value" yaml:"value"`
}

// reportOut is a Report as JSON prints it: the same keys in the same order,
// with Windows a list even when the provider knows of none, never null, so
// consumers always read an array. It names every field instead of embedding
// Report because an embedded struct's keys print first, which would move
// windows after extra.
type reportOut struct {
	Provider string   `json:"provider"`
	Plan     string   `json:"plan,omitempty"`
	Account  *Account `json:"account,omitempty"`
	Windows  []Window `json:"windows"`
	Extra    []Fact   `json:"extra,omitempty"`
}

func (r Report) out() reportOut {
	out := reportOut{Provider: r.Provider, Plan: r.Plan, Account: r.Account, Windows: r.Windows, Extra: r.Extra}
	if out.Windows == nil {
		out.Windows = []Window{}
	}
	return out
}

func (r Report) MarshalJSON() ([]byte, error) { return json.Marshal(r.out()) }
