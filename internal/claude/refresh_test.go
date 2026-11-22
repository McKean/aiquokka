package claude

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/McKean/aiquokka/internal/httpx/httpxtest"
)

// A failed refresh names every endpoint's answer in try order — endpoint,
// status and body — not just the last.
func TestRefreshErrorNamesEachEndpoint(t *testing.T) {
	original := refreshEndpoints
	t.Cleanup(func() { refreshEndpoints = original })
	refreshEndpoints = []string{
		"https://first.example.com/v1/oauth/token",
		"https://second.example.com/v1/oauth/token",
	}
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		if strings.Contains(r.URL.Host, "first") {
			return http.StatusBadRequest, `{"error":"invalid_grant","error_description":"Refresh token expired"}`
		}
		return http.StatusNotFound, `{"error":"not_found"}`
	})

	err := refresh(context.Background(), &oauth{AccessToken: "old", RefreshToken: "dead-refresh", ExpiresAt: 1})
	if err == nil {
		t.Fatal("refresh with two failing endpoints: expected an error")
	}
	msg := err.Error()
	first := strings.Index(msg, "https://first.example.com/v1/oauth/token")
	second := strings.Index(msg, "https://second.example.com/v1/oauth/token")
	switch {
	case first < 0 || second < 0:
		t.Fatalf("error names only one endpoint: %v", err)
	case first > second:
		t.Fatalf("endpoints out of try order: %v", err)
	}
	for _, want := range []string{"Bad Request", "Refresh token expired", "Not Found", "not_found"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}
