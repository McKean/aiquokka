package grok

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/httpx/httpxtest"
	"github.com/McKean/aiquokka/internal/usage"
)

func storeLogin(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GROK_HOME", dir)
	t.Setenv("GROK_CLI_CHAT_PROXY_BASE_URL", "")
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertUntouched(t *testing.T, path, stored string) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, []byte(stored)) {
		t.Fatalf("stored login was rewritten:\n%s", after)
	}
}

func TestFetchWithoutRefreshLeavesExpiredTokenAlone(t *testing.T) {
	stored := `{"https://auth.x.ai::u":{"key":"old","refresh_token":"old-refresh","oidc_client_id":"c","expires_at":"2020-01-01T00:00:00Z"}}`
	path := storeLogin(t, stored)
	httpxtest.Refuse(t)

	_, err := Fetch(usage.WithoutRefresh(context.Background()))
	if !errors.Is(err, usage.ErrNoRefresh) {
		t.Fatalf("err = %v, want ErrNoRefresh", err)
	}
	assertUntouched(t, path, stored)
}

// A token the file calls valid can still be rejected; the 401 retry must not
// refresh either.
func TestFetchWithoutRefreshDoesNotRefreshOnRejection(t *testing.T) {
	stored := `{"https://auth.x.ai::u":{"key":"old","refresh_token":"old-refresh","oidc_client_id":"c"}}`
	path := storeLogin(t, stored)
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		return http.StatusUnauthorized, `{}`
	})

	_, err := Fetch(usage.WithoutRefresh(context.Background()))
	if !errors.Is(err, usage.ErrNoRefresh) {
		t.Fatalf("err = %v, want ErrNoRefresh", err)
	}
	assertUntouched(t, path, stored)
}

func TestFetchWindowIDLengthAndAccount(t *testing.T) {
	storeLogin(t, `{"https://auth.x.ai::u":{"key":"tok","user_id":"user-1","email":"a@example.com"}}`)
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		if strings.HasPrefix(r.URL.Path, "/v1/user") {
			return http.StatusOK, `{"subscriptionTier":"XPremium"}`
		}
		return http.StatusOK, `{"config":{"creditUsagePercent":12,"currentPeriod":{
			"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-10-01T00:00:00Z","end":"2026-10-08T00:00:00Z"}}}`
	})

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 1 {
		t.Fatalf("windows = %+v", report.Windows)
	}
	if w := report.Windows[0]; w.ID != "weekly" || w.Label != "Weekly" || w.Duration != 7*24*time.Hour {
		t.Fatalf("window = %+v", w)
	}
	if a := report.Account; a == nil || a.ID != "user-1" || a.Email != "a@example.com" {
		t.Fatalf("account = %+v", a)
	}
}

func TestPeriodID(t *testing.T) {
	for in, want := range map[string]string{
		"USAGE_PERIOD_TYPE_WEEKLY":  "weekly",
		"USAGE_PERIOD_TYPE_MONTHLY": "monthly",
		"":                          "usage",
	} {
		if got := periodID(in); got != want {
			t.Errorf("periodID(%q) = %q, want %q", in, got, want)
		}
	}
}
