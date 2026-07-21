package grok

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

// tokenEndpoint derives the OIDC token endpoint from the issuer (auth.x.ai for
// the default xAI login; enterprise SSO issuers work the same way).
func (a *account) tokenEndpoint() string {
	issuer := a.OIDCIssuer
	if issuer == "" {
		issuer = "https://auth.x.ai"
	}
	return strings.TrimRight(issuer, "/") + "/oauth2/token"
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// refresh exchanges the refresh token for a fresh access token, updates the
// account in place, and persists it back to auth.json under storeKey.
func refresh(ctx context.Context, a *account, storeKey string) error {
	if a.RefreshToken == "" || a.OIDCClientID == "" {
		return fmt.Errorf("Grok token expired and cannot be refreshed (missing refresh token) — run `grok` to re-login")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {a.RefreshToken},
		"client_id":     {a.OIDCClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenEndpoint(), strings.NewReader(form.Encode()))
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
		if strings.Contains(string(body), "invalid_grant") {
			return fmt.Errorf("Grok refresh token is no longer valid — run `grok` to re-login")
		}
		return fmt.Errorf("refreshing Grok token: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return err
	}
	if tr.AccessToken == "" {
		return fmt.Errorf("Grok refresh returned no access token")
	}

	a.Key = tr.AccessToken
	if tr.RefreshToken != "" {
		a.RefreshToken = tr.RefreshToken
	}
	if tr.ExpiresIn > 0 {
		a.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).UTC().Format(time.RFC3339Nano)
	}
	if err := persist(a, storeKey); err != nil {
		fmt.Fprintf(os.Stderr, "aiquokka: warning: could not persist refreshed Grok token: %v\n", err)
	}
	return nil
}

// persist writes the refreshed token fields back into the account entry,
// preserving every other field and account in auth.json.
func persist(a *account, storeKey string) error {
	path, err := authPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	entry, ok := raw[storeKey]
	if !ok {
		return fmt.Errorf("account key %q vanished from auth.json", storeKey)
	}
	set := func(k string, v any) {
		if b, err := json.Marshal(v); err == nil {
			entry[k] = b
		}
	}
	set("key", a.Key)
	set("refresh_token", a.RefreshToken)
	set("expires_at", a.ExpiresAt)
	raw[storeKey] = entry

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
