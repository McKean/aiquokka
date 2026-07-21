// Package claude reads Claude Code credentials and fetches subscription usage.
package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

// credentialsPath is ~/.claude/.credentials.json.
func credentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", ".credentials.json"), nil
}

// oauth mirrors the claudeAiOauth object in .credentials.json.
type oauth struct {
	AccessToken           string   `json:"accessToken"`
	RefreshToken          string   `json:"refreshToken"`
	ExpiresAt             int64    `json:"expiresAt"`
	RefreshTokenExpiresAt int64    `json:"refreshTokenExpiresAt"`
	Scopes                []string `json:"scopes"`
	SubscriptionType      string   `json:"subscriptionType"`
	RateLimitTier         string   `json:"rateLimitTier"`
}

type credentialsFile struct {
	ClaudeAiOauth oauth `json:"claudeAiOauth"`
}

// expired reports whether the access token is past its expiry.
func (o oauth) expired(now time.Time) bool {
	if o.ExpiresAt == 0 {
		return false
	}
	return now.UnixMilli() >= o.ExpiresAt
}

// loadCredentials reads and parses the Claude credentials file.
func loadCredentials() (*oauth, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, usage.NotConfigured("no Claude credentials at %s — run `claude` to log in", path)
		}
		return nil, err
	}
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if cf.ClaudeAiOauth.AccessToken == "" {
		return nil, usage.NotConfigured("no OAuth access token in %s", path)
	}
	return &cf.ClaudeAiOauth, nil
}
