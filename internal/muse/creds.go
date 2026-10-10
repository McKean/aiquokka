// Package muse reads the Muse CLI's stored device login and reports its
// subscription quota from the CLI's own key-mint call.
package muse

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/McKean/aiquokka/internal/usage"
)

// authFile is the Muse CLI's metadata file. MUSE_AUTH_PATH overrides its
// location; otherwise it lives under the XDG config home.
func authFile() string {
	if v := strings.TrimSpace(os.Getenv("MUSE_AUTH_PATH")); v != "" {
		return v
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "muse", "auth.json")
}

// cliAuth mirrors the Muse CLI's auth.json. providers.meta holds the account
// login: mechanism "oauth" with the device-code access token inline on Linux,
// while on macOS the token may live in the Keychain instead.
type cliAuth struct {
	Providers map[string]struct {
		Mechanism   string `json:"mechanism"`
		AccessToken string `json:"access_token"`
	} `json:"providers"`
}

// loadToken returns the device-code OAuth access token for the meta provider.
// An inline providers.meta.access_token wins; on macOS a login without one
// falls back to the CLI's Keychain item. Inference keys (LLM|…) and dashboard
// keys (LLM_…) cannot read quota — only the device login can — so META_API_KEY
// is deliberately not a credential source here.
func loadToken(ctx context.Context) (string, error) {
	path := authFile()
	if data, err := os.ReadFile(path); err == nil {
		var auth cliAuth
		if err := json.Unmarshal(data, &auth); err != nil {
			return "", &jsonError{path: path, err: err}
		}
		meta := auth.Providers["meta"]
		if token := strings.TrimSpace(meta.AccessToken); token != "" {
			return token, nil
		}
		if meta.Mechanism != "" {
			if token, err := keychainLoader(ctx); err == nil {
				return token, nil
			} else if unread, ok := asKeychainUnreadable(err); ok {
				return "", usage.NotConfigured("Muse Keychain item %s could not be read (%s) — answer the permission dialog on the Mac desktop with \"Always Allow\", or run `security unlock-keychain` if the Keychain is locked", keychainService, unread.reason)
			} else if !isKeychainNotFound(err) {
				return "", err
			}
		}
		return "", usage.NotConfigured("no Muse login in %s — run `muse login`", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}

	// No metadata file at all: macOS may still hold a Keychain-only login.
	// An unreadable item unwraps to errKeychainNotFound, so it falls through
	// to the generic report below: with no auth.json naming a login there is
	// nothing to attribute it to.
	if token, err := keychainLoader(ctx); err == nil {
		return token, nil
	} else if !isKeychainNotFound(err) {
		return "", err
	}
	if path == "" {
		return "", usage.NotConfigured("no Muse login found — run `muse login`")
	}
	return "", usage.NotConfigured("no Muse login at %s — run `muse login`", path)
}

// jsonError reports a corrupt auth.json without printing its contents: the
// file holds secrets, so even a parse offset could leak their length.
type jsonError struct {
	path string
	err  error
}

func (e *jsonError) Error() string { return "parsing " + e.path + ": invalid JSON" }
func (e *jsonError) Unwrap() error { return e.err }

// keychainAccessToken extracts the OAuth token from the CLI's Keychain item
// payload, which it stores as JSON. A bare token is accepted too.
func keychainAccessToken(payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", errKeychainNotFound
	}
	var v struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(payload, &v) == nil && strings.TrimSpace(v.AccessToken) != "" {
		return strings.TrimSpace(v.AccessToken), nil
	}
	if trimmed := strings.TrimSpace(string(payload)); trimmed != "" && !strings.ContainsAny(trimmed, " \t\r\n{") {
		return trimmed, nil
	}
	return "", errKeychainNotFound
}
