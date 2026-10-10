package muse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/McKean/aiquokka/internal/httpx"
	"github.com/McKean/aiquokka/internal/usage"
)

// keyEndpoint is the CLI's own key-mint call: the same request it makes at
// startup and for /usage. Besides the minted inference key it reports the
// subscription tier and, while a window is active, its usage. Overridable for
// tests.
var keyEndpoint = "https://api.meta.ai/muse-code/key"

// mintResponse holds the only fields aiquokka reads from the mint response.
// Everything else — the minted key, account identity, payment metadata — is
// discarded after parsing and never written anywhere.
type mintResponse struct {
	IsSubsActive   *bool      `json:"is_subs_active"`
	RequirePayment bool       `json:"require_payment"`
	SubsTierName   string     `json:"subs_tier_name"`
	SubsUsage      *subsUsage `json:"subs_usage"`
}

type subsUsage struct {
	Tier   string          `json:"tier"`
	Window *windowSnapshot `json:"window"`
	Weekly *windowSnapshot `json:"weekly"`
}

// windowSnapshot is one subscription window. Numbers arrive as JSON numbers
// (used_percent a percentage, resets_at unix seconds), but the fields stay
// loose so a misshapen window is skipped instead of failing the whole read.
type windowSnapshot struct {
	UsedPercent        any `json:"used_percent"`
	WindowDurationMins any `json:"window_duration_mins"`
	ResetsAt           any `json:"resets_at"`
}

// Fetch reports the Muse subscription quota: the rolling 5-hour window and
// the weekly window from the CLI's key-mint call. The stored login is only
// read, never written — there is no token refresh to forbid under --no-refresh.
func Fetch(ctx context.Context) (*usage.Report, error) {
	token, err := loadToken(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := mint(ctx, token)
	if err != nil {
		return nil, err
	}
	return reportFromMint(resp), nil
}

// mintTTL is how long one key-mint response is reused within the process. The
// endpoint is rate-limited, and quota needs no fresher read than this.
const mintTTL = 5 * time.Minute

var (
	mintMu     sync.Mutex
	mintBody   []byte
	mintToken  [32]byte
	mintExpiry time.Time
)

// resetMintCache forgets the cached response. Tests use it for isolation.
func resetMintCache() {
	mintMu.Lock()
	defer mintMu.Unlock()
	mintBody, mintExpiry = nil, time.Time{}
}

// mint POSTs the device login to the key endpoint and parses the quota block,
// reusing a fresh cached response for the same token.
func mint(ctx context.Context, token string) (*mintResponse, error) {
	sum := sha256.Sum256([]byte(token))
	mintMu.Lock()
	if time.Now().Before(mintExpiry) && mintBody != nil && mintToken == sum {
		body := mintBody
		mintMu.Unlock()
		return parseMint(body)
	}
	mintMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, keyEndpoint, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Api-Version", "1.0.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "aiquokka")

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	// The response body can carry the minted key and account identity, so it
	// never appears in an error.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("Muse login was rejected (%s) — run `muse login` again (API keys cannot read quota)", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST %s: %s", keyEndpoint, resp.Status)
	}
	parsed, err := parseMint(body)
	if err != nil {
		return nil, err
	}

	mintMu.Lock()
	mintBody, mintToken, mintExpiry = body, sum, time.Now().Add(mintTTL)
	mintMu.Unlock()
	return parsed, nil
}

func parseMint(body []byte) (*mintResponse, error) {
	var out mintResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding Muse quota: %w", err)
	}
	return &out, nil
}

// reportFromMint converts the quota block to the shared model. A missing
// subs_usage is meaningful — Meta omits it while the 5-hour window is idle —
// so it stays unknown rather than becoming a fake 0%.
func reportFromMint(resp *mintResponse) *usage.Report {
	report := &usage.Report{Provider: "Muse", Plan: resp.SubsTierName}
	if report.Plan == "" && resp.SubsUsage != nil {
		report.Plan = resp.SubsUsage.Tier
	}

	active := resp.IsSubsActive == nil || *resp.IsSubsActive
	switch {
	case resp.RequirePayment:
		report.Extra = append(report.Extra, usage.Fact{Label: "Subscription", Value: "billing incomplete"})
	case !active:
		report.Extra = append(report.Extra, usage.Fact{Label: "Subscription", Value: "inactive"})
	}

	if !active || resp.SubsUsage == nil {
		if active && !resp.RequirePayment {
			report.Extra = append(report.Extra, usage.Fact{Label: "Usage", Value: "idle — no active window reported"})
		}
		return report
	}

	if w := rollingWindow(resp.SubsUsage.Window); w != nil {
		report.Windows = append(report.Windows, *w)
	}
	if w := weeklyWindow(resp.SubsUsage.Weekly); w != nil {
		report.Windows = append(report.Windows, *w)
	}
	if len(report.Windows) == 0 {
		report.Extra = append(report.Extra, usage.Fact{Label: "Usage", Value: "idle — no active window reported"})
	}
	return report
}

// defaultRollingMins is the rolling window's length. The payload confirms it
// via window_duration_mins; the default covers responses that leave it out.
const defaultRollingMins = 300

func rollingWindow(snap *windowSnapshot) *usage.Window {
	used, ok := number(snapField(snap, func(s *windowSnapshot) any { return s.UsedPercent }))
	if !ok {
		return nil
	}
	mins, ok := number(snapField(snap, func(s *windowSnapshot) any { return s.WindowDurationMins }))
	if !ok || mins <= 0 {
		mins = defaultRollingMins
	}
	dur := time.Duration(mins) * time.Minute
	id := usage.LengthID(dur)
	if id == "" {
		id = "window"
	}
	return &usage.Window{
		ID:          id,
		Label:       labelFor(dur, "5h"),
		UsedPercent: &used,
		ResetsAt:    resetsAt(snapField(snap, func(s *windowSnapshot) any { return s.ResetsAt })),
		Duration:    dur,
	}
}

func weeklyWindow(snap *windowSnapshot) *usage.Window {
	used, ok := number(snapField(snap, func(s *windowSnapshot) any { return s.UsedPercent }))
	if !ok {
		return nil
	}
	return &usage.Window{
		ID:          "weekly",
		Label:       "Weekly",
		UsedPercent: &used,
		ResetsAt:    resetsAt(snapField(snap, func(s *windowSnapshot) any { return s.ResetsAt })),
		Duration:    7 * 24 * time.Hour,
	}
}

// snapField reads one field from a possibly-nil snapshot.
func snapField(snap *windowSnapshot, get func(*windowSnapshot) any) any {
	if snap == nil {
		return nil
	}
	return get(snap)
}

// labelFor names the rolling window by its length, keeping the familiar "5h"
// for the standard window.
func labelFor(dur time.Duration, fallback string) string {
	if dur == 5*time.Hour {
		return "5h"
	}
	if id := usage.LengthID(dur); id != "" {
		return id
	}
	return fallback
}

// number coerces a JSON number to float64. Anything else (a string, null, a
// missing field) reads as absent so the window is skipped, never faked.
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// resetsAt parses a reset timestamp: unix seconds or milliseconds, a unix
// timestamp in a string, or an ISO/RFC3339 instant. Unreadable values stay
// unknown (the zero time) without dropping the window's percentage.
func resetsAt(v any) time.Time {
	switch t := v.(type) {
	case float64, json.Number:
		secs, ok := number(v)
		if !ok {
			return time.Time{}
		}
		// Seconds vs milliseconds: 1e11 ms is 1973, 1e11 s is year 5138.
		if secs >= 1e11 {
			secs /= 1000
		}
		if secs < 0 || secs > 1<<40 {
			return time.Time{}
		}
		s, frac := int64(secs), secs-float64(int64(secs))
		return time.Unix(s, int64(frac*1e9)).UTC()
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return time.Time{}
		}
		if allDigits(s) {
			n, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return time.Time{}
			}
			return resetsAt(n)
		}
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
