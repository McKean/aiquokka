// Package antigravity reads Google Antigravity usage directly from CloudCode's
// HTTP API using the OAuth credentials stored by the `agy` CLI, refreshing
// expired tokens automatically unless --no-refresh is set.
package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/httpx"
	"github.com/McKean/aiquokka/internal/usage"
)

const (
	loadCodeAssistURL       = "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	retrieveQuotaSummaryURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"

	// CloudCode's retrieveUserQuotaSummary endpoint filters returned quota
	// groups by User-Agent; only antigravity/cli/... receives all groups
	// (Gemini Models, Claude and GPT models, and Gemini 4 Argon).
	apiUserAgent = "antigravity/cli/1.5.7"
)

var errUnauthorized = errors.New("Antigravity token rejected (401)")

type loadCodeAssistRequest struct {
	Metadata clientMetadata `json:"metadata"`
}

type clientMetadata struct {
	IDEType    string `json:"ideType"`
	Platform   string `json:"platform"`
	PluginType string `json:"pluginType"`
}

type tierInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type loadCodeAssistResponse struct {
	CloudaicompanionProject string    `json:"cloudaicompanionProject"`
	CurrentTier             *tierInfo `json:"currentTier"`
	PaidTier                *tierInfo `json:"paidTier"`
}

type quotaSummaryRequest struct {
	Project string `json:"project,omitempty"`
}

type quotaSummaryResponse struct {
	Groups []quotaGroup `json:"groups"`
}

type quotaGroup struct {
	DisplayName string        `json:"displayName"`
	Buckets     []quotaBucket `json:"buckets"`
}

type quotaBucket struct {
	BucketID          string  `json:"bucketId"`
	DisplayName       string  `json:"displayName"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
	Disabled          bool    `json:"disabled"`
}

// Fetch loads the stored Antigravity OAuth credentials, refreshes the access
// token if needed, queries CloudCode directly for tier and quota summary, and
// converts the response into aiquokka's common usage model.
func Fetch(ctx context.Context) (*usage.Report, error) {
	auth, err := loadAuth(ctx)
	if err != nil {
		return nil, err
	}

	refreshed := false
	if isExpiringSoon(auth) {
		if err := refresh(ctx, auth); err != nil {
			return nil, err
		}
		refreshed = true
	}

	caBody, err := postWithRefreshRetry(ctx, auth, &refreshed, loadCodeAssistURL, loadCodeAssistRequest{
		Metadata: clientMetadata{
			IDEType:    "ANTIGRAVITY",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("Antigravity loadCodeAssist: %w", err)
	}
	var ca loadCodeAssistResponse
	if err := json.Unmarshal(caBody, &ca); err != nil {
		return nil, fmt.Errorf("decoding Antigravity loadCodeAssist: %w", err)
	}

	summaryBody, err := postWithRefreshRetry(ctx, auth, &refreshed, retrieveQuotaSummaryURL, quotaSummaryRequest{
		Project: ca.CloudaicompanionProject,
	})
	if err != nil {
		return nil, fmt.Errorf("Antigravity quota summary: %w", err)
	}

	report, err := parseQuotaSummary(summaryBody)
	if err != nil {
		return nil, err
	}
	report.Plan = planName(&ca)
	report.Account = usage.NewAccount("", strings.TrimSpace(auth.IDTokenEmail))
	return report, nil
}

func postWithRefreshRetry(ctx context.Context, auth *authFile, refreshed *bool, endpoint string, payload any) ([]byte, error) {
	body, err := postJSON(ctx, endpoint, auth.Token.AccessToken, payload)
	if errors.Is(err, errUnauthorized) && !*refreshed {
		if err := refresh(ctx, auth); err != nil {
			return nil, err
		}
		*refreshed = true
		body, err = postJSON(ctx, endpoint, auth.Token.AccessToken, payload)
	}
	if errors.Is(err, errUnauthorized) {
		return nil, fmt.Errorf("token rejected — run `agy` to re-login")
	}
	return body, err
}

func postJSON(ctx context.Context, endpoint, accessToken string, payload any) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", apiUserAgent)

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, extractAPIError(body))
	}
	return body, nil
}

func extractAPIError(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return strings.TrimSpace(envelope.Error.Message)
	}
	return strings.TrimSpace(string(body))
}

func planName(ca *loadCodeAssistResponse) string {
	if ca != nil {
		if ca.PaidTier != nil && strings.TrimSpace(ca.PaidTier.Name) != "" {
			return strings.TrimSpace(ca.PaidTier.Name)
		}
		if ca.CurrentTier != nil && strings.TrimSpace(ca.CurrentTier.Name) != "" {
			return strings.TrimSpace(ca.CurrentTier.Name)
		}
	}
	return "Usage"
}

// bucketLabel turns a CloudCode bucket's window length into the bar label
// ("Weekly", "5h"). Unknown windows keep the display name without the trailing
// " Remaining".
func bucketLabel(window, name string) string {
	switch window {
	case "weekly":
		return "Weekly"
	case "5h":
		return "5h"
	}
	return strings.TrimSuffix(name, " Remaining")
}

func parseQuotaSummary(raw []byte) (*usage.Report, error) {
	var resp quotaSummaryResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decoding Antigravity quota response: %w", err)
	}

	report := &usage.Report{Provider: "Antigravity", Plan: "Usage"}
	totalBuckets := 0
	for _, group := range resp.Groups {
		for _, b := range group.Buckets {
			totalBuckets++
			if b.Disabled {
				continue
			}
			used := math.Max(0, math.Min(100, (1.0-b.RemainingFraction)*100.0))
			bucketName := b.DisplayName
			if bucketName == "" {
				bucketName = b.BucketID
			}
			// Keep the <group>/<bucket-display-name> slug format used by prior
			// versions so callers filtering by Window.ID keep working.
			w := usage.Window{
				ID:          usage.Slug(group.DisplayName) + "/" + usage.Slug(bucketName),
				Label:       fmt.Sprintf("%s - %s", group.DisplayName, bucketLabel(b.Window, bucketName)),
				UsedPercent: &used,
			}
			if b.ResetTime != "" {
				if t, err := time.Parse(time.RFC3339Nano, b.ResetTime); err == nil {
					w.ResetsAt = t
				}
			}
			switch b.Window {
			case "weekly":
				w.Duration = 7 * 24 * time.Hour
			case "5h":
				w.Duration = 5 * time.Hour
			}
			report.Windows = append(report.Windows, w)
		}
	}

	if len(report.Windows) == 0 {
		if totalBuckets > 0 {
			report.Extra = append(report.Extra, usage.Fact{
				Label: "Usage",
				Value: "disabled — no active quota windows reported",
			})
			return report, nil
		}
		return nil, fmt.Errorf("no quota buckets in Antigravity quota response")
	}
	return report, nil
}
