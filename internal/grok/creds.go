// Package grok reads xAI Grok CLI credentials and fetches usage limits.
package grok

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

// authPath is ~/.grok/auth.json.
func authPath() (string, error) {
	if v := os.Getenv("GROK_HOME"); v != "" {
		return filepath.Join(v, "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grok", "auth.json"), nil
}

// account mirrors one entry in auth.json (keyed by "https://auth.x.ai::<uuid>").
type account struct {
	Key           string `json:"key"`
	AuthMode      string `json:"auth_mode"`
	UserID        string `json:"user_id"`
	Email         string `json:"email"`
	TeamID        string `json:"team_id"`
	RefreshToken  string `json:"refresh_token"`
	ExpiresAt     string `json:"expires_at"`
	OIDCIssuer    string `json:"oidc_issuer"`
	OIDCClientID  string `json:"oidc_client_id"`
	PrincipalType string `json:"principal_type"`
}

// expired reports whether the access token is at/near expiry (60s margin).
func (a *account) expired(now time.Time) bool {
	if a.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, a.ExpiresAt)
	if err != nil {
		return false
	}
	return now.After(t.Add(-60 * time.Second))
}

// loadAccount reads auth.json and returns the x.ai account entry along with the
// map key it was stored under (needed to persist a refreshed token).
func loadAccount() (*account, string, error) {
	path, err := authPath()
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", usage.NotConfigured("no Grok credentials at %s — run `grok` to log in", path)
		}
		return nil, "", err
	}
	var accounts map[string]account
	if err := json.Unmarshal(data, &accounts); err != nil {
		return nil, "", fmt.Errorf("parsing %s: %w", path, err)
	}
	for key, acc := range accounts {
		if !strings.Contains(key, "x.ai") {
			continue
		}
		if acc.Key == "" {
			continue
		}
		a := acc
		return &a, key, nil
	}
	return nil, "", usage.NotConfigured("no usable x.ai account in %s — run `grok` to log in", path)
}
