package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/McKean/aiquokka/internal/httpx"
)

// refreshEndpoint and codexClientID are Codex CLI's OAuth values.
const (
	refreshEndpoint = "https://auth.openai.com/oauth/token"
	codexClientID   = "app_EMoamEEZ73f0CkXaXp7hrann"
)

type refreshRequest struct {
	ClientID     string `json:"client_id"`
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

type refreshResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// refresh exchanges the refresh token, updates auth in place, and persists it.
func refresh(ctx context.Context, auth *authFile) error {
	reqBody, _ := json.Marshal(refreshRequest{
		ClientID:     codexClientID,
		GrantType:    "refresh_token",
		RefreshToken: auth.Tokens.RefreshToken,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aiquokka")

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, string(body))
	}
	var rr refreshResponse
	if err := json.Unmarshal(body, &rr); err != nil {
		return err
	}
	if rr.AccessToken == "" {
		return fmt.Errorf("refresh returned no access token")
	}

	auth.Tokens.AccessToken = rr.AccessToken
	if rr.IDToken != "" {
		auth.Tokens.IDToken = rr.IDToken
	}
	if rr.RefreshToken != "" {
		auth.Tokens.RefreshToken = rr.RefreshToken
	}
	auth.LastRefresh = time.Now().UTC().Format(time.RFC3339)

	if err := persist(auth); err != nil {
		fmt.Fprintf(os.Stderr, "aiquokka: warning: could not persist refreshed Codex token: %v\n", err)
	}
	return nil
}

// persist writes updated token fields back to auth.json, preserving unknown keys.
func persist(auth *authFile) error {
	path, err := authPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	tokBytes, err := json.Marshal(auth.Tokens)
	if err != nil {
		return err
	}
	raw["tokens"] = tokBytes
	if lr, err := json.Marshal(auth.LastRefresh); err == nil {
		raw["last_refresh"] = lr
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
