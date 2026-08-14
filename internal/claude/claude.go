package claude

import (
	"context"
	"net/http"
	"time"

	"github.com/McKean/aiquokka/internal/httpx"
	"github.com/McKean/aiquokka/internal/usage"
)

// usageEndpoint is Claude Code's undocumented subscription-usage endpoint.
const usageEndpoint = "https://api.anthropic.com/api/oauth/usage"

// userAgent must be a claude-code/<ver> string or the endpoint drops the
// request into an aggressively rate-limited bucket (429s).
const userAgent = "claude-code/2.0.32"

const week = 7 * 24 * time.Hour

// usageResponse is the endpoint's payload. Every window we render comes from
// `limits`: the top-level five_hour and seven_day objects restate its first
// two entries, and Fable's cap appears nowhere else.
type usageResponse struct {
	Limits []limit `json:"limits"`
}

// limit is one entry of the `limits` array.
type limit struct {
	Kind     string   `json:"kind"`
	Percent  *float64 `json:"percent"`
	ResetsAt string   `json:"resets_at"`
}

// Fetch reads local credentials (refreshing the token if needed) and returns
// the current Claude subscription usage.
func Fetch(ctx context.Context) (*usage.Report, error) {
	creds, err := loadCredentials(ctx)
	if err != nil {
		return nil, err
	}
	if creds.expired(time.Now()) {
		if err := refresh(ctx, creds); err != nil {
			return nil, err
		}
	}

	var resp usageResponse
	err = httpx.GetJSON(ctx, usageEndpoint, &resp, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+creds.AccessToken)
		r.Header.Set("anthropic-beta", "oauth-2025-04-20")
		r.Header.Set("User-Agent", userAgent)
		r.Header.Set("Accept", "application/json")
	})
	if err != nil {
		return nil, err
	}

	report := &usage.Report{
		Provider: "Claude",
		Plan:     plan(creds),
	}
	for _, w := range []*usage.Window{
		resp.window("session", "5h", 5*time.Hour),
		resp.window("weekly_all", "Weekly", week),
		resp.window("weekly_scoped", "Weekly Fable", week),
	} {
		if w != nil {
			report.Windows = append(report.Windows, *w)
		}
	}
	return report, nil
}

func plan(o *oauth) string {
	switch {
	case o.SubscriptionType != "" && o.RateLimitTier != "":
		return o.SubscriptionType + "/" + o.RateLimitTier
	case o.SubscriptionType != "":
		return o.SubscriptionType
	case o.RateLimitTier != "":
		return o.RateLimitTier
	}
	return ""
}

// window converts the limit of the given kind into the shared model, or nil
// when the account does not have that limit.
func (resp *usageResponse) window(kind, label string, d time.Duration) *usage.Window {
	for _, l := range resp.Limits {
		if l.Kind != kind || l.Percent == nil {
			continue
		}
		out := usage.Window{Label: label, UsedPercent: l.Percent, Duration: d}
		if t, err := time.Parse(time.RFC3339, l.ResetsAt); err == nil {
			out.ResetsAt = t
		}
		return &out
	}
	return nil
}
