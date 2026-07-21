// Package httpx holds a shared HTTP client and small JSON helpers.
package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the shared HTTP client for all providers.
var Client = &http.Client{Timeout: 15 * time.Second}

// GetJSON performs a GET and decodes a JSON body into out. The mutate callback
// may set headers (auth, etc.) on the request before it is sent.
func GetJSON(ctx context.Context, url string, out any, mutate func(*http.Request)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if mutate != nil {
		mutate(req)
	}
	resp, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s: %s", url, resp.Status, truncate(string(body), 300))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding %s: %w", url, err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
