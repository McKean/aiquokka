// Package claude reads Claude Code credentials and fetches subscription usage.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

// configDir is $CLAUDE_CONFIG_DIR when set, as Claude Code does, else ~/.claude.
func configDir() (string, error) {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// credentialsPath is .credentials.json in the Claude config dir.
func credentialsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".credentials.json"), nil
}

// accountPath is where Claude Code records the account: .claude.json inside
// $CLAUDE_CONFIG_DIR when set, otherwise ~/.claude.json.
func accountPath() (string, error) {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return filepath.Join(v, ".claude.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
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
	keychain     bool
	keychainItem []byte
}

func (s credentialSource) isKeychain() bool {
	return s.keychain || len(s.keychainItem) != 0
}

var errKeychainCredentialsNotFound = errors.New("Claude Code Keychain credentials not found")

// These private seams let the refresh path be tested without touching a real
// Keychain. Production code always uses the platform implementations.
var readKeychainCredentials = loadKeychainCredentials
var readKeychainItem = loadKeychainCredential
var updateKeychainItem = persistKeychainCredentials

// expired reports whether the access token is past its expiry.
func (o oauth) expired(now time.Time) bool {
	if o.ExpiresAt == 0 {
		return false
	}
	return now.UnixMilli() >= o.ExpiresAt
}

// loadCredentials returns the live credential across both stores: the file in
// the Claude config dir when it holds a live OAuth access token, else the
// macOS Keychain item when it is live. The Keychain is only read when the
// file is missing, unparseable, or expired. When neither store is live, the
// file keeps today's place if it parses, so its own refresh path is unchanged.
func loadCredentials(ctx context.Context) (*oauth, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}

	var fileCreds *oauth
	var fileErr error
	if data, err := os.ReadFile(path); err == nil {
		if o, _, perr := parseCredentials(data); perr == nil {
			fileCreds = o
		} else {
			fileErr = fmt.Errorf("parsing %s: %w", path, perr)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	} else {
		fileErr = err
	}

	if fileCreds != nil && !fileCreds.expired(time.Now()) {
		return fileCreds, nil
	}

	data, item, err := readKeychainCredentials(ctx)
	switch {
	case err == nil:
		o, _, perr := parseCredentials(data)
		if perr != nil {
			if fileCreds != nil {
				return fileCreds, nil
			}
			return nil, perr
		}
		o.source = credentialSource{keychain: true, keychainItem: item}
		if o.expired(time.Now()) && fileCreds != nil {
			return fileCreds, nil
		}
		return o, nil
	case errors.Is(err, context.Canceled):
		return nil, err
	case fileCreds != nil:
		return fileCreds, nil
	case !errors.Is(err, errKeychainCredentialsNotFound):
		return nil, err
	case fileErr != nil && !os.IsNotExist(fileErr):
		return nil, fileErr
	default:
		return nil, usage.NotConfigured("no Claude credentials at %s — run `claude` to log in", path)
	}
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

// loadAccount reads who is logged in from the account file, where Claude Code
// records the account at login (the credential itself, file or Keychain,
// holds only tokens). It returns nil when the file or the entry is missing.
func loadAccount() *usage.Account {
	path, err := accountPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		OAuthAccount struct {
			AccountUUID  string `json:"accountUuid"`
			EmailAddress string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	return usage.NewAccount(cfg.OAuthAccount.AccountUUID, cfg.OAuthAccount.EmailAddress)
}
