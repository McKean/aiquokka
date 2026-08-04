package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/McKean/aiquokka/internal/httpx"
	"github.com/McKean/aiquokka/internal/usage"
)

type appData struct {
	OAuthToken string `json:"oauth_token"`
}

type copilotResponse struct {
	CopilotPlan       string `json:"copilot_plan"`
	QuotaResetDateUTC string `json:"quota_reset_date_utc"`
	QuotaSnapshots    struct {
		PremiumInteractions *struct {
			PercentRemaining float64 `json:"percent_remaining"`
		} `json:"premium_interactions"`
		Chat *struct {
			PercentRemaining float64 `json:"percent_remaining"`
		} `json:"chat"`
		Completions *struct {
			PercentRemaining float64 `json:"percent_remaining"`
		} `json:"completions"`
	} `json:"quota_snapshots"`
}

func getToken() (string, error) {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "github-copilot", "apps.json"))
		paths = append(paths, filepath.Join(home, ".config", "github-copilot", "hosts.json"))
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(configDir, "github-copilot", "apps.json"))
		paths = append(paths, filepath.Join(configDir, "github-copilot", "hosts.json"))
	}

	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var apps map[string]appData
		if err := json.Unmarshal(b, &apps); err != nil {
			continue
		}
		for _, app := range apps {
			if app.OAuthToken != "" {
				return app.OAuthToken, nil
			}
		}
	}
	return "", usage.NotConfigured("no Copilot credentials found in ~/.config/github-copilot/ (try signing in with your IDE)")
}

// Fetch reads local Copilot credentials and returns the current usage.
func Fetch(ctx context.Context) (*usage.Report, error) {
	token, err := getToken()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/copilot_internal/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/1.96.2")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.26.7")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.26.7")
	req.Header.Set("X-Github-Api-Version", "2025-04-01")

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, string(body))
	}
	var out copilotResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding usage: %w", err)
	}

	report := &usage.Report{Provider: "Copilot", Plan: out.CopilotPlan}

	var resetsAt time.Time
	var duration time.Duration
	if out.QuotaResetDateUTC != "" {
		if t, err := time.Parse(time.RFC3339, out.QuotaResetDateUTC); err == nil {
			resetsAt = t
			// Copilot windows are monthly. Approximate the start as exactly one month prior.
			start := t.AddDate(0, -1, 0)
			duration = t.Sub(start)
		}
	}

	addWindow := func(label string, snap *struct {
		PercentRemaining float64 `json:"percent_remaining"`
	}) {
		if snap != nil {
			used := 100.0 - snap.PercentRemaining
			report.Windows = append(report.Windows, usage.Window{
				Label:       label,
				UsedPercent: &used,
				ResetsAt:    resetsAt,
				Duration:    duration,
			})
		}
	}

	addWindow("Premium", out.QuotaSnapshots.PremiumInteractions)
	addWindow("Chat", out.QuotaSnapshots.Chat)
	addWindow("Completions", out.QuotaSnapshots.Completions)

	return report, nil
}
