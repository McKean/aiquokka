// Package muse reports the Muse subscription quota: from the CLI's own
// key-mint call where its stored device login is readable, and from the CLI
// itself where it is not.
package muse

import (
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
// while on macOS the CLI keeps the token in its own Keychain item.
type cliAuth struct {
	Providers map[string]struct {
		Mechanism   string `json:"mechanism"`
		AccessToken string `json:"access_token"`
	} `json:"providers"`
}

// loadToken returns the device-code OAuth access token for the meta provider
// when auth.json holds it inline. An empty token with no error means the
// login, if there is one, is not readable from the file: auth.json names a
// login without a token, or there is no auth.json. The caller then asks the
// CLI itself (see fetchFromCLI); aiquokka never reads the CLI's Keychain
// item. Inference keys (LLM|…) and dashboard keys (LLM_…) cannot read quota —
// only the device login can — so META_API_KEY is deliberately not a
// credential source here.
func loadToken() (string, error) {
	path := authFile()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var auth cliAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return "", &jsonError{path: path, err: err}
	}
	meta := auth.Providers["meta"]
	if token := strings.TrimSpace(meta.AccessToken); token != "" {
		return token, nil
	}
	if meta.Mechanism == "" {
		return "", usage.NotConfigured("no Muse login in %s — run `muse login`", path)
	}
	return "", nil
}

// jsonError reports a corrupt auth.json without printing its contents: the
// file holds secrets, so even a parse offset could leak their length.
type jsonError struct {
	path string
	err  error
}

func (e *jsonError) Error() string { return "parsing " + e.path + ": invalid JSON" }
func (e *jsonError) Unwrap() error { return e.err }
