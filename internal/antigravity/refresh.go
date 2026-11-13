package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/httpx"
	"github.com/McKean/aiquokka/internal/usage"
)

const oauthTokenEndpoint = "https://oauth2.googleapis.com/token"

type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	IDToken      string `json:"id_token"`
}

type oauthError struct {
	status string
	code   string
	desc   string
	raw    string
}

func (e *oauthError) Error() string {
	if e.raw != "" {
		return fmt.Sprintf("refreshing Antigravity token: %s: %s", e.status, e.raw)
	}
	return fmt.Sprintf("refreshing Antigravity token: %s", e.status)
}

func (e *oauthError) isClientMismatch() bool {
	return e.code == "invalid_client" || e.code == "unauthorized_client"
}

// refresh exchanges the refresh token for a new access token, updates auth in
// place, and persists it back to its credential store.
func refresh(ctx context.Context, auth *authFile) error {
	if !usage.RefreshAllowed(ctx) {
		return usage.NoRefresh("Antigravity", "agy")
	}
	if auth.Token.RefreshToken == "" {
		return fmt.Errorf("Antigravity access token expired and no refresh token available — run `agy` to re-login")
	}

	src, candidates, err := openClientSource()
	if err != nil {
		return err
	}

	rr, usedClient, err := tryRefreshCandidates(ctx, auth.Token.RefreshToken, candidates)
	if err != nil && src != nil && src.cached != nil {
		var oe *oauthError
		if errors.As(err, &oe) && oe.isClientMismatch() {
			if scanned, scanErr := src.scan(); scanErr == nil && len(scanned) > 0 {
				rr, usedClient, err = tryRefreshCandidates(ctx, auth.Token.RefreshToken, scanned)
			}
		}
	}
	if err != nil {
		return err
	}
	if src != nil && (src.cached == nil || *src.cached != usedClient) {
		saveCachedClient(src, usedClient)
	}

	auth.Token.AccessToken = rr.AccessToken
	if rr.RefreshToken != "" {
		auth.Token.RefreshToken = rr.RefreshToken
	}
	if rr.TokenType != "" {
		auth.Token.TokenType = rr.TokenType
	}
	if rr.ExpiresIn > 0 {
		auth.Token.ExpiresIn = rr.ExpiresIn
		auth.Token.Expiry = time.Now().Add(time.Duration(rr.ExpiresIn) * time.Second).Format(time.RFC3339Nano)
	}
	if auth.IDTokenEmail == "" && rr.IDToken != "" {
		auth.IDTokenEmail = idTokenEmail(rr.IDToken)
	}

	if err := persist(ctx, auth); err != nil {
		// Non-fatal: the refreshed token is still usable for this request.
		fmt.Fprintf(os.Stderr, "aiquokka: warning: could not persist refreshed Antigravity token: %v\n", err)
	}
	return nil
}

func tryRefreshCandidates(ctx context.Context, refreshToken string, candidates []oauthClient) (*refreshResponse, oauthClient, error) {
	var lastErr error
	for _, client := range candidates {
		rr, err := doRefresh(ctx, refreshToken, client)
		if err == nil {
			return rr, client, nil
		}
		lastErr = err
		var oe *oauthError
		if !errors.As(err, &oe) || !oe.isClientMismatch() {
			return nil, oauthClient{}, err
		}
	}
	return nil, oauthClient{}, lastErr
}

func doRefresh(ctx context.Context, refreshToken string, client oauthClient) (*refreshResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {client.ID},
		"client_secret": {client.Secret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", apiUserAgent)

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refreshing Antigravity token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &payload)
		return nil, &oauthError{
			status: resp.Status,
			code:   payload.Error,
			desc:   payload.ErrorDescription,
			raw:    strings.TrimSpace(string(body)),
		}
	}

	var rr refreshResponse
	if err := json.Unmarshal(body, &rr); err != nil {
		return nil, fmt.Errorf("decoding Antigravity token refresh: %w", err)
	}
	if rr.AccessToken == "" {
		return nil, fmt.Errorf("Antigravity token refresh returned no access token")
	}
	return &rr, nil
}

// persist writes the updated token block back to whichever store (keyring or
// file) supplied auth, preserving any unknown keys already present.
func persist(ctx context.Context, auth *authFile) error {
	if auth.source == sourceKeyring {
		existing := auth.raw
		if current, err := readKeyring(ctx); err == nil && len(current) > 0 {
			existing = current
		}
		out, err := mergeAuthJSON(existing, auth, false)
		if err != nil {
			return err
		}
		if err := writeKeyring(ctx, out); err != nil {
			return err
		}
		auth.raw = out
		return nil
	}

	existing, err := os.ReadFile(auth.path)
	if err != nil {
		return err
	}
	out, err := mergeAuthJSON(existing, auth, true)
	if err != nil {
		return err
	}
	auth.raw = out
	return writeAtomic0600(auth.path, out)
}

func mergeAuthJSON(existing []byte, auth *authFile, indent bool) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(existing, &raw); err != nil {
		return nil, err
	}

	tokMap := make(map[string]json.RawMessage)
	if prev, ok := raw["token"]; ok {
		_ = json.Unmarshal(prev, &tokMap)
	}
	setJSONField(tokMap, "access_token", auth.Token.AccessToken)
	setJSONFieldIfNonEmpty(tokMap, "token_type", auth.Token.TokenType)
	setJSONFieldIfNonEmpty(tokMap, "refresh_token", auth.Token.RefreshToken)
	setJSONFieldIfNonEmpty(tokMap, "expiry", auth.Token.Expiry)
	if auth.Token.ExpiresIn > 0 {
		setJSONField(tokMap, "expires_in", auth.Token.ExpiresIn)
	}
	tokBytes, err := json.Marshal(tokMap)
	if err != nil {
		return nil, err
	}
	raw["token"] = tokBytes
	setJSONFieldIfNonEmpty(raw, "id_token_email", auth.IDTokenEmail)

	if indent {
		out, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(out, '\n'), nil
	}
	return json.Marshal(raw)
}

func setJSONField(m map[string]json.RawMessage, key string, val any) {
	if b, err := json.Marshal(val); err == nil {
		m[key] = b
	}
}

func setJSONFieldIfNonEmpty(m map[string]json.RawMessage, key, val string) {
	if val != "" {
		setJSONField(m, key, val)
	}
}

func writeAtomic0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".antigravity-oauth-token-*")
	if err != nil {
		return os.WriteFile(path, data, 0o600)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
