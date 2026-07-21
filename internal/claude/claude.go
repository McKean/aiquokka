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

// window mirrors the {utilization, resets_at} shape the endpoint returns.
type window struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

type usageResponse struct {
	FiveHour       *window `json:"five_hour"`
	SevenDay       *window `json:"seven_day"`
	SevenDayOpus   *window `json:"seven_day_opus"`
	SevenDaySonnet *window `json:"seven_day_sonnet"`
}

// Fetch reads local credentials (refreshing the token if needed) and returns
// the current Claude subscription usage.
func Fetch(ctx context.Context) (*usage.Report, error) {
	creds, err := loadCredentials()
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
	if w := toWindow("5h", resp.FiveHour); w != nil {
		report.Windows = append(report.Windows, *w)
	}
	if w := toWindow("Weekly", resp.SevenDay); w != nil {
		report.Windows = append(report.Windows, *w)
	}
	if w := toWindow("Weekly Opus", resp.SevenDayOpus); w != nil {
		report.Windows = append(report.Windows, *w)
	}
	if w := toWindow("Weekly Sonnet", resp.SevenDaySonnet); w != nil {
		report.Windows = append(report.Windows, *w)
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

// toWindow converts an API window into the shared model, or nil when absent.
func toWindow(label string, w *window) *usage.Window {
	if w == nil || w.Utilization == nil {
		return nil
	}
	out := usage.Window{Label: label, UsedPercent: w.Utilization}
	if label == "5h" {
		out.Duration = 5 * time.Hour
	} else {
		out.Duration = 7 * 24 * time.Hour
	}
	if w.ResetsAt != "" {
		if t, err := time.Parse(time.RFC3339, w.ResetsAt); err == nil {
			out.ResetsAt = t
		}
	}
	return &out
}
