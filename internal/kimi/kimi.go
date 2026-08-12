package kimi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/httpx"
	"github.com/McKean/aiquokka/internal/usage"
)

// baseURL is the Kimi Code coding platform default.
func baseURL() string { return "https://api.kimi.com/coding/v1" }

// num accepts a JSON count that may be encoded as a string ("100") or a number
// (100); absent/empty stays unset.
type num struct {
	set bool
	v   int64
}

func (n *num) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return fmt.Errorf("kimi: unparseable count %q", s)
		}
		v = int64(f)
	}
	n.set, n.v = true, v
	return nil
}

func (n num) ptr() *int64 {
	if !n.set {
		return nil
	}
	v := n.v
	return &v
}

// row is one usage detail row. Counts are strings on the wire.
type row struct {
	Name       string `json:"name"`
	Title      string `json:"title"`
	Scope      string `json:"scope"`
	Used       num    `json:"used"`
	Limit      num    `json:"limit"`
	Remaining  num    `json:"remaining"`
	ResetAt    string `json:"reset_at"`
	ResetAt2   string `json:"resetAt"`
	ResetTime  string `json:"reset_time"`
	ResetTime2 string `json:"resetTime"`
}

type window struct {
	Duration int64  `json:"duration"`
	TimeUnit string `json:"timeUnit"`
}

// usagesResponse covers the `usage` (weekly) + `limits[]` (windows) shape.
type usagesResponse struct {
	Usage  *row `json:"usage"`
	Limits []struct {
		Detail row    `json:"detail"`
		Window window `json:"window"`
	} `json:"limits"`
}

// Fetch reads the Kimi Code token (refreshing it if needed) and returns the
// current usage windows.
func Fetch(ctx context.Context) (*usage.Report, error) {
	creds, err := load()
	if err != nil {
		return nil, err
	}
	if creds.expired(time.Now()) {
		if err := refresh(ctx, creds); err != nil {
			return nil, err
		}
	}

	resp, err := getUsages(ctx, creds)
	if err != nil {
		return nil, err
	}

	report := &usage.Report{Provider: "Kimi"}
	// Each limits[] entry is a rolling window (e.g. the 5h window).
	for _, l := range resp.Limits {
		if w := toWindow(windowLabel(l.Detail, l.Window), l.Detail); w != nil {
			w.Duration = windowDuration(l.Window)
			report.Windows = append(report.Windows, *w)
		}
	}
	// The top-level `usage` row is the weekly summary.
	if resp.Usage != nil {
		if w := toWindow("Weekly", *resp.Usage); w != nil {
			w.Duration = 7 * 24 * time.Hour
			report.Windows = append(report.Windows, *w)
		}
	}
	return report, nil
}

// windowDuration converts a window spec (duration + timeUnit) to a Duration.
func windowDuration(w window) time.Duration {
	if w.Duration <= 0 {
		return 0
	}
	switch normalizeUnit(w.TimeUnit) {
	case "HOUR":
		return time.Duration(w.Duration) * time.Hour
	case "DAY":
		return time.Duration(w.Duration) * 24 * time.Hour
	default: // MINUTE
		return time.Duration(w.Duration) * time.Minute
	}
}

// getUsages performs the authed GET, refreshing and retrying once on a 401.
func getUsages(ctx context.Context, c *credentials) (*usagesResponse, error) {
	resp, status, err := doUsages(ctx, c.bearer)
	if err == nil {
		return resp, nil
	}
	if status == http.StatusUnauthorized && c.refreshable {
		if rerr := refresh(ctx, c); rerr != nil {
			return nil, rerr
		}
		resp, _, err = doUsages(ctx, c.bearer)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}
	return nil, err
}

// doUsages issues the request and returns the parsed body plus HTTP status.
func doUsages(ctx context.Context, bearer string) (*usagesResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURLFromEnv()+"/usages", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aiquokka")

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("%s/usages: %s: %s", baseURLFromEnv(), resp.Status, strings.TrimSpace(string(body)))
	}
	var out usagesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decoding usages: %w", err)
	}
	return &out, resp.StatusCode, nil
}

// windowLabel derives a friendly label from a detail name or the window spec
// (e.g. duration 300 / MINUTE -> "5h").
func windowLabel(d row, w window) string {
	if name := firstNonEmpty(d.Name, d.Title, d.Scope); name != "" {
		return name
	}
	if w.Duration > 0 && w.TimeUnit != "" {
		mins := w.Duration
		switch normalizeUnit(w.TimeUnit) {
		case "HOUR":
			mins = w.Duration * 60
		case "DAY":
			mins = w.Duration * 60 * 24
		}
		switch {
		case mins%(60*24) == 0:
			return itoa(mins/(60*24)) + "d"
		case mins%60 == 0:
			return itoa(mins/60) + "h"
		default:
			return itoa(mins) + "m"
		}
	}
	return "window"
}

// normalizeUnit turns "TIME_UNIT_MINUTE" / "MINUTE" into "MINUTE".
func normalizeUnit(u string) string {
	u = strings.ToUpper(u)
	u = strings.TrimPrefix(u, "TIME_UNIT_")
	return u
}

// toWindow converts a usage row into the shared model. Absolute counts remain
// available to structured output, while UsedPercent keeps rendered output
// consistent with the other providers.
func toWindow(label string, d row) *usage.Window {
	used := d.Used.ptr()
	limit := d.Limit.ptr()
	if used == nil && limit != nil && d.Remaining.set {
		v := *limit - d.Remaining.v
		used = &v
	}
	if used == nil && limit == nil {
		return nil
	}
	out := usage.Window{Label: label, Used: used, Limit: limit}
	if used != nil && limit != nil && *limit > 0 {
		percent := float64(*used) / float64(*limit) * 100
		out.UsedPercent = &percent
	}
	if reset := firstNonEmpty(d.ResetAt, d.ResetAt2, d.ResetTime, d.ResetTime2); reset != "" {
		if t, err := time.Parse(time.RFC3339, reset); err == nil {
			out.ResetsAt = t
		}
	}
	return &out
}

func baseURLFromEnv() string {
	if v := getenv("KIMI_CODE_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := getenv("KIMI_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return baseURL()
}
