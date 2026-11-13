package antigravity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

const (
	tokenEnvVar  = "ANTIGRAVITY_TOKEN_PATH"
	expiryBuffer = 60 * time.Second
)

type authSource uint8

const (
	sourceFile authSource = iota
	sourceKeyring
)

type tokenData struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Expiry       string `json:"expiry,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
}

type authFile struct {
	Token        tokenData `json:"token"`
	AuthMethod   string    `json:"auth_method,omitempty"`
	IDTokenEmail string    `json:"id_token_email,omitempty"`
	source       authSource
	path         string
	raw          []byte
}

// tokenPath returns $ANTIGRAVITY_TOKEN_PATH if set, or the default path where
// agy stores its OAuth credentials (~/.gemini/antigravity-cli/antigravity-oauth-token).
func tokenPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv(tokenEnvVar)); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token"), nil
}

// loadAuth reads Antigravity OAuth credentials. When ANTIGRAVITY_TOKEN_PATH is
// set, only that file is read. Otherwise it checks both agy's OS keyring item
// and its file fallback, choosing the fresher credential when both exist.
func loadAuth(ctx context.Context) (*authFile, error) {
	path, err := tokenPath()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(os.Getenv(tokenEnvVar)) != "" {
		auth, err := loadAuthFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, usage.NotConfigured("no Antigravity credentials — run `agy` and sign in first")
			}
			return nil, err
		}
		return auth, nil
	}

	var krAuth *authFile
	krData, krErr := readKeyring(ctx)
	if errors.Is(krErr, context.Canceled) {
		return nil, krErr
	}
	if krErr == nil {
		krAuth, _ = parseAuthBytes(krData, sourceKeyring, "")
	}

	fileAuth, fileErr := loadAuthFile(path)
	switch {
	case krAuth != nil && fileAuth != nil:
		if hasNewerExpiry(fileAuth, krAuth) {
			return fileAuth, nil
		}
		return krAuth, nil
	case krAuth != nil:
		return krAuth, nil
	case fileAuth != nil:
		return fileAuth, nil
	case errors.Is(krErr, errKeyringLocked) || errors.Is(krErr, errKeyringTimeout):
		return nil, krErr
	case fileErr != nil && !os.IsNotExist(fileErr):
		return nil, fileErr
	default:
		return nil, usage.NotConfigured("no Antigravity credentials — run `agy` and sign in first")
	}
}

func loadAuthFile(path string) (*authFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	auth, err := parseAuthBytes(data, sourceFile, path)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return auth, nil
}

func parseAuthBytes(data []byte, source authSource, path string) (*authFile, error) {
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, err
	}
	if auth.Token.AccessToken == "" && auth.Token.RefreshToken == "" {
		return nil, usage.NotConfigured("no Antigravity token found — run `agy` and sign in first")
	}
	auth.source = source
	auth.path = path
	auth.raw = data
	return &auth, nil
}

func hasNewerExpiry(candidate, baseline *authFile) bool {
	cExp, cErr := time.Parse(time.RFC3339Nano, candidate.Token.Expiry)
	if cErr != nil {
		return false
	}
	bExp, bErr := time.Parse(time.RFC3339Nano, baseline.Token.Expiry)
	if bErr != nil {
		return true
	}
	return cExp.After(bExp)
}

// isExpiringSoon reports whether the access token is missing or expires within
// expiryBuffer (or cannot be parsed).
func isExpiringSoon(auth *authFile) bool {
	if auth.Token.AccessToken == "" || auth.Token.Expiry == "" {
		return true
	}
	exp, err := time.Parse(time.RFC3339Nano, auth.Token.Expiry)
	if err != nil {
		return true
	}
	return !exp.After(time.Now().Add(expiryBuffer))
}

// idTokenEmail extracts the email claim from a JWT id_token without verifying
// its signature (used only as a display hint).
func idTokenEmail(jwt string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.Email)
}
