// Package kimi reads Kimi Code credentials and fetches subscription usage.
package kimi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

// credentials is a resolved Kimi bearer token, optionally refreshable.
type credentials struct {
	bearer string // access token or static sk-kimi-… key

	// refreshable fields (only set for OAuth credential files).
	refreshable  bool
	file         string
	refreshToken string
	expiresAt    int64 // unix seconds; 0 if unknown
}

// expired reports whether the access token is at/near its expiry (60s margin).
func (c *credentials) expired(now time.Time) bool {
	if !c.refreshable || c.expiresAt == 0 {
		return false
	}
	return now.Unix() >= c.expiresAt-60
}

// oauthFile mirrors ~/.kimi-code/credentials/<provider>.json.
type oauthFile struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// load resolves credentials from, in order:
//   - $KIMI_API_KEY / $KIMI_CODING_API_KEY (static key, not refreshable)
//   - the OAuth credentials the kimi-code / kimi CLI wrote on login
func load() (*credentials, error) {
	for _, env := range []string{"KIMI_API_KEY", "KIMI_CODING_API_KEY"} {
		if v := os.Getenv(env); v != "" {
			return &credentials{bearer: v}, nil
		}
	}
	for _, dir := range shareDirs() {
		if c := loadFromDir(dir); c != nil {
			return c, nil
		}
	}
	return nil, usage.NotConfigured("no Kimi credentials found — set KIMI_API_KEY (sk-kimi-…) or log in with the kimi CLI")
}

// shareDirs are the candidate CLI data dirs, most specific first. The official
// CLI uses ~/.kimi; the "kimi-code" build uses ~/.kimi-code. $KIMI_SHARE_DIR
// overrides both.
func shareDirs() []string {
	if v := os.Getenv("KIMI_SHARE_DIR"); v != "" {
		return []string{v}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".kimi-code"),
		filepath.Join(home, ".kimi"),
	}
}

// loadFromDir returns credentials from the first parseable credentials/*.json
// in dir, or nil if none is found.
func loadFromDir(dir string) *credentials {
	entries, err := os.ReadDir(filepath.Join(dir, "credentials"))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, "credentials", e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var f oauthFile
		if json.Unmarshal(data, &f) != nil || f.AccessToken == "" {
			continue
		}
		return &credentials{
			bearer:       f.AccessToken,
			refreshable:  f.RefreshToken != "",
			file:         path,
			refreshToken: f.RefreshToken,
			expiresAt:    f.ExpiresAt,
		}
	}
	return nil
}
