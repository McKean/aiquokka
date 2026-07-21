package grok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/httpx"
	"github.com/McKean/aiquokka/internal/usage"
)

// baseURL is the Grok CLI chat proxy (override via GROK_CLI_CHAT_PROXY_BASE_URL).
func baseURL() string {
	if v := os.Getenv("GROK_CLI_CHAT_PROXY_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://cli-chat-proxy.grok.com/v1"
}

// billingResponse mirrors GET /billing?format=credits — the data behind the
// Grok CLI's `/usage` view.
type billingResponse struct {
	Config struct {
		CreditUsagePercent *float64 `json:"creditUsagePercent"` // omitted when 0
		CurrentPeriod      *struct {
			Type  string `json:"type"` // USAGE_PERIOD_TYPE_WEEKLY / _MONTHLY
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"currentPeriod"`
	} `json:"config"`
}

// userResponse is the subset of /user?include=subscription we surface.
type userResponse struct {
	SubscriptionTier  string `json:"subscriptionTier"`
	HasGrokCodeAccess bool   `json:"hasGrokCodeAccess"`
}

// Fetch reports Grok's usage-limit window (weekly for subscription accounts)
// plus the subscription tier.
func Fetch(ctx context.Context) (*usage.Report, error) {
	acc, storeKey, err := loadAccount()
	if err != nil {
		return nil, err
	}
	if acc.expired(time.Now()) {
		if err := refresh(ctx, acc, storeKey); err != nil {
			return nil, err
		}
	}

	report := &usage.Report{Provider: "Grok"}

	// Subscription tier + Grok Code access (best-effort; don't fail the whole
	// report if this call errors).
	if u, err := getUser(ctx, acc, storeKey); err == nil {
		report.Plan = u.SubscriptionTier
		access := "no"
		if u.HasGrokCodeAccess {
			access = "yes"
		}
		report.Extra = append(report.Extra, usage.Fact{Label: "Grok Code", Value: access})
	}

	// The usage-limit window.
	bill, err := getBilling(ctx, acc, storeKey)
	if err != nil {
		return nil, err
	}
	if w := billingWindow(bill); w != nil {
		report.Windows = append(report.Windows, *w)
	}
	return report, nil
}

// billingWindow converts the credits config into a usage window. The percentage
// is "credits used" (omitted by the server when 0), and the reset is the end of
// the current period.
func billingWindow(b *billingResponse) *usage.Window {
	cp := b.Config.CurrentPeriod
	if cp == nil {
		return nil
	}
	pct := 0.0
	if b.Config.CreditUsagePercent != nil {
		pct = *b.Config.CreditUsagePercent
	}
	w := usage.Window{Label: periodLabel(cp.Type), UsedPercent: &pct}
	var start, end time.Time
	if cp.End != "" {
		if t, err := time.Parse(time.RFC3339, cp.End); err == nil {
			w.ResetsAt = t
			end = t
		}
	}
	if cp.Start != "" {
		if t, err := time.Parse(time.RFC3339, cp.Start); err == nil {
			start = t
		}
	}
	if !start.IsZero() && !end.IsZero() && end.After(start) {
		w.Duration = end.Sub(start)
	}
	return &w
}

// periodLabel turns USAGE_PERIOD_TYPE_WEEKLY into "Weekly".
func periodLabel(t string) string {
	switch strings.ToUpper(strings.TrimPrefix(t, "USAGE_PERIOD_TYPE_")) {
	case "WEEKLY":
		return "Weekly"
	case "MONTHLY":
		return "Monthly"
	case "DAILY":
		return "Daily"
	case "":
		return "Usage"
	default:
		return strings.Title(strings.ToLower(strings.TrimPrefix(t, "USAGE_PERIOD_TYPE_")))
	}
}

// getBilling calls /billing?format=credits, refreshing/retrying once on 401/403.
func getBilling(ctx context.Context, acc *account, storeKey string) (*billingResponse, error) {
	var out billingResponse
	if err := authedGet(ctx, acc, storeKey, "/billing?format=credits", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// getUser calls /user?include=subscription with the same retry behavior.
func getUser(ctx context.Context, acc *account, storeKey string) (*userResponse, error) {
	var out userResponse
	if err := authedGet(ctx, acc, storeKey, "/user?include=subscription", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// authedGet performs a bearer GET, refreshing the token and retrying once on a
// 401/403.
func authedGet(ctx context.Context, acc *account, storeKey, path string, out any) error {
	status, err := doGet(ctx, acc.Key, path, out)
	if err == nil {
		return nil
	}
	if (status == http.StatusUnauthorized || status == http.StatusForbidden) && acc.RefreshToken != "" {
		if rerr := refresh(ctx, acc, storeKey); rerr != nil {
			return rerr
		}
		_, err = doGet(ctx, acc.Key, path, out)
	}
	return err
}

func doGet(ctx context.Context, bearer, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL()+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decoding %s: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}
