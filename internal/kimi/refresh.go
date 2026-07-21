package kimi

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

// Kimi Code's OAuth device-flow token endpoint and public client id (extracted
// from the kimi-code CLI). Overridable via env for other builds.
const (
	kimiTokenEndpoint = "https://auth.kimi.com/api/oauth/token"
	kimiClientID      = "17e5f671-d194-4dfb-9706-5516cb48c098"
)

func tokenEndpoint() string {
	if v := os.Getenv("KIMI_OAUTH_TOKEN_URL"); v != "" {
		return v
	}
	return kimiTokenEndpoint
}

func clientID() string {
	if v := os.Getenv("KIMI_OAUTH_CLIENT_ID"); v != "" {
		return v
	}
	return kimiClientID
}

// refresh exchanges the refresh token for a fresh access token, updates c in
// place, and writes the result back to the credentials file.
func refresh(ctx context.Context, c *credentials) error {
	if c.refreshToken == "" {
		return fmt.Errorf("Kimi token expired and no refresh token available — run `kimi` to re-login")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.refreshToken},
		"client_id":     {clientID()},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint(), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("refreshing Kimi token: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out oauthFile
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	if out.AccessToken == "" {
		return fmt.Errorf("Kimi refresh returned no access token")
	}

	c.bearer = out.AccessToken
	if out.RefreshToken != "" {
		c.refreshToken = out.RefreshToken
	}
	if out.ExpiresIn > 0 {
		c.expiresAt = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second).Unix()
	}
	if err := c.persist(out); err != nil {
		fmt.Fprintf(os.Stderr, "aiquokka: warning: could not persist refreshed Kimi token: %v\n", err)
	}
	return nil
}

// persist writes the refreshed token fields back to the credentials file,
// preserving any other keys already present.
func (c *credentials) persist(out oauthFile) error {
	if c.file == "" {
		return nil
	}
	data, err := os.ReadFile(c.file)
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	set := func(key string, v any) {
		if b, err := json.Marshal(v); err == nil {
			raw[key] = b
		}
	}
	set("access_token", c.bearer)
	set("refresh_token", c.refreshToken)
	set("expires_at", c.expiresAt)
	if out.ExpiresIn > 0 {
		set("expires_in", out.ExpiresIn)
	}
	if out.TokenType != "" {
		set("token_type", out.TokenType)
	}
	if out.Scope != "" {
		set("scope", out.Scope)
	}

	merged, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.file, merged, 0o600)
}
