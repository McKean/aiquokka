// Package codex reads OpenAI Codex CLI credentials and fetches usage limits.
package codex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/McKean/aiquokka/internal/usage"
)

// authPath is ~/.codex/auth.json.
func authPath() (string, error) {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return filepath.Join(v, "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "auth.json"), nil
}

// tokens mirrors the tokens object in auth.json.
type tokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
}

type authFile struct {
	AuthMode     string `json:"auth_mode"`
	OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	Tokens       tokens `json:"tokens"`
	LastRefresh  string `json:"last_refresh"`
}

// loadAuth reads and parses the Codex auth file.
func loadAuth() (*authFile, error) {
	path, err := authPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, usage.NotConfigured("no Codex credentials at %s — run `codex login` first", path)
		}
		return nil, err
	}
	var af authFile
	if err := json.Unmarshal(data, &af); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if af.Tokens.AccessToken == "" {
		return nil, usage.NotConfigured("no ChatGPT access token in %s (API-key auth has no usage limits)", path)
	}
	return &af, nil
}

// account says whose login auth.json holds: the ChatGPT account id next to the
// tokens, and the email claim of the stored id_token. The token is our own
// stored login, read for one claim, so its signature is not checked.
func (af *authFile) account() *usage.Account {
	return usage.NewAccount(af.Tokens.AccountID, idTokenEmail(af.Tokens.IDToken))
}

// idTokenEmail returns the email claim of a JWT, or "" when it has none or the
// token does not parse.
func idTokenEmail(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Email
}
