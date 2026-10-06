package kimi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/httpx/httpxtest"
	"github.com/McKean/aiquokka/internal/usage"
)

// storeLogin writes an OAuth credential file into a temporary share dir.
func storeLogin(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KIMI_SHARE_DIR", dir)
	t.Setenv("KIMI_API_KEY", "")
	t.Setenv("KIMI_CODING_API_KEY", "")
	path := filepath.Join(dir, "credentials", "kimi-code.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
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
	stored := `{"access_token":"old","refresh_token":"old-refresh","expires_at":1}`
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
	stored := `{"access_token":"old","refresh_token":"old-refresh","expires_at":0}`
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

func TestFetchWindowIDsAndLengths(t *testing.T) {
	storeLogin(t, `{"access_token":"tok"}`)
	httpxtest.Stub(t, func(*http.Request) (int, string) {
		return http.StatusOK, `{
			"usage": {"used": "78", "limit": "100"},
			"limits": [{"detail": {"used": "15", "limit": "100"}, "window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"}}]
		}`
	})

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 2 {
		t.Fatalf("windows = %+v", report.Windows)
	}
	if w := report.Windows[0]; w.ID != "5h" || w.Duration != 5*time.Hour {
		t.Fatalf("limit window = %+v", w)
	}
	if w := report.Windows[1]; w.ID != "weekly" || w.Duration != 7*24*time.Hour {
		t.Fatalf("weekly window = %+v", w)
	}
	if report.Account != nil {
		t.Fatalf("Kimi's login names no account, got %+v", report.Account)
	}
}

// The ID must not follow a display name the API sends for the window.
func TestLimitIDIgnoresTheLabel(t *testing.T) {
	spec := window{Duration: 5, TimeUnit: "HOUR"}
	if label := windowLabel(row{Name: "Rolling window"}, spec); label != "Rolling window" {
		t.Fatalf("label = %q", label)
	}
	if id := limitID(windowDuration(spec)); id != "5h" {
		t.Fatalf("id = %q, want 5h", id)
	}
	if id := limitID(0); id != "limit" {
		t.Fatalf("id without a length = %q, want limit", id)
	}
}
