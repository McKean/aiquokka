// Package claude reads Claude Code credentials and fetches subscription usage.
package claude

import (
	"encoding/json"
	"errors"
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

	// source identifies where the credential was loaded so a refreshed token
	// is written back to that same store. It is deliberately not serialized.
	source credentialSource
}

type credentialsFile struct {
	ClaudeAiOauth oauth `json:"claudeAiOauth"`
}

type credentialSource struct {
	keychainItem []byte
}

var errKeychainCredentialsNotFound = errors.New("Claude Code Keychain credentials not found")
var errMultipleKeychainCredentials = errors.New("multiple Claude Code credentials found in Keychain")

// These private seams let the refresh path be tested without touching a real
// Keychain. Production code always uses the platform implementations.
var readKeychainItem = loadKeychainCredential
var updateKeychainItem = persistKeychainCredentials

// expired reports whether the access token is past its expiry.
func (o oauth) expired(now time.Time) bool {
	if o.ExpiresAt == 0 {
		return false
	}
	return now.UnixMilli() >= o.ExpiresAt
}

// loadCredentials reads and parses the Claude credentials file.
func loadCredentials() (*oauth, error) {
	if data, item, err := loadKeychainCredentials(); err == nil {
		o, _, err := parseCredentials(data)
		if err != nil {
			return nil, err
		}
		o.source = credentialSource{keychainItem: item}
		return o, nil
	} else if !errors.Is(err, errKeychainCredentialsNotFound) {
		return nil, err
	}

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
	o, _, err := parseCredentials(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return o, nil
}

// mergeCredentials preserves fields owned by Claude Code while applying the
// OAuth values that Aiquokka refreshed.
func mergeCredentials(data []byte, direct bool, o *oauth) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	var target map[string]json.RawMessage
	if direct {
		target = raw
	} else {
		encoded, ok := raw["claudeAiOauth"]
		if !ok {
			return nil, fmt.Errorf("no claudeAiOauth credential")
		}
		if err := json.Unmarshal(encoded, &target); err != nil {
			return nil, err
		}
	}
	set := func(key string, value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		target[key] = encoded
		return nil
	}
	if err := set("accessToken", o.AccessToken); err != nil {
		return nil, err
	}
	if err := set("refreshToken", o.RefreshToken); err != nil {
		return nil, err
	}
	if err := set("expiresAt", o.ExpiresAt); err != nil {
		return nil, err
	}
	if !direct {
		encoded, err := json.Marshal(target)
		if err != nil {
			return nil, err
		}
		raw["claudeAiOauth"] = encoded
	}
	return json.MarshalIndent(raw, "", "  ")
}

// parseCredentials accepts both the historical file envelope and a bare OAuth
// object, which lets the Keychain backend preserve whichever representation
// the installed Claude Code version uses.
func parseCredentials(data []byte) (*oauth, bool, error) {
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err == nil && cf.ClaudeAiOauth.AccessToken != "" {
		return &cf.ClaudeAiOauth, false, nil
	}

	var o oauth
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, false, err
	}
	if o.AccessToken == "" {
		return nil, false, fmt.Errorf("no OAuth access token")
	}
	return &o, true, nil
}
