package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/httpx"
)

// claudeCodeClientID is Claude Code's public OAuth client id.
const claudeCodeClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

// refreshEndpoints are tried in order; Anthropic migrated from console to platform.
var refreshEndpoints = []string{
	"https://platform.claude.com/v1/oauth/token",
	"https://console.anthropic.com/v1/oauth/token",
}

type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// refresh exchanges the refresh token for a fresh access token, updates o in
// place, and persists the result back to the credentials file.
func refresh(ctx context.Context, o *oauth) error {
	if o.RefreshToken == "" {
		return fmt.Errorf("access token expired and no refresh token available — run `claude` to re-login")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {o.RefreshToken},
		"client_id":     {claudeCodeClientID},
	}

	var lastErr error
	for _, endpoint := range refreshEndpoints {
		rr, err := doRefresh(ctx, endpoint, form)
		if err != nil {
			lastErr = err
			continue
		}
		o.AccessToken = rr.AccessToken
		if rr.RefreshToken != "" {
			o.RefreshToken = rr.RefreshToken
		}
		if rr.ExpiresIn > 0 {
			o.ExpiresAt = time.Now().Add(time.Duration(rr.ExpiresIn) * time.Second).UnixMilli()
		}
		if err := persist(o); err != nil {
			// Non-fatal: we still have a usable in-memory token.
			fmt.Fprintf(os.Stderr, "aiquokka: warning: could not persist refreshed token: %v\n", err)
		}
		return nil
	}
	return fmt.Errorf("refreshing Claude token: %w", lastErr)
}

func doRefresh(ctx context.Context, endpoint string, form url.Values) (*refreshResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var rr refreshResponse
	if err := json.Unmarshal(body, &rr); err != nil {
		return nil, err
	}
	if rr.AccessToken == "" {
		return nil, fmt.Errorf("refresh returned no access token")
	}
	return &rr, nil
}

// persist writes the updated oauth block back into the credentials file,
// preserving any other keys already present.
func persist(o *oauth) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Merge into the existing claudeAiOauth object so we don't drop fields.
	var existing map[string]json.RawMessage
	if b, ok := raw["claudeAiOauth"]; ok {
		_ = json.Unmarshal(b, &existing)
	}
	if existing == nil {
		existing = map[string]json.RawMessage{}
	}
	set := func(key string, v any) {
		if b, err := json.Marshal(v); err == nil {
			existing[key] = b
		}
	}
	set("accessToken", o.AccessToken)
	set("refreshToken", o.RefreshToken)
	set("expiresAt", o.ExpiresAt)

	merged, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	raw["claudeAiOauth"] = merged
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
