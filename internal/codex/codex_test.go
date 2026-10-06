package codex

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/httpx/httpxtest"
	"github.com/McKean/aiquokka/internal/usage"
)

// idToken builds an unsigned JWT with the given payload.
func idToken(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + ".sig"
}

func storeLogin(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Codex learns that a token has expired from a 401; under --no-refresh that
// must end the fetch without a token request and without a write.
func TestFetchWithoutRefreshLeavesRejectedTokenAlone(t *testing.T) {
	stored := `{"tokens":{"access_token":"old","refresh_token":"old-refresh","account_id":"acc-1"}}`
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
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, []byte(stored)) {
		t.Fatalf("stored login was rewritten:\n%s", after)
	}
}

func TestFetchWindowIDsLengthsAndAccount(t *testing.T) {
	storeLogin(t, `{"tokens":{"access_token":"tok","account_id":"acc-1","id_token":"`+idToken(`{"email":"a@example.com","sub":"x"}`)+`"}}`)
	httpxtest.Stub(t, func(*http.Request) (int, string) {
		return http.StatusOK, `{"plan_type":"plus","rate_limit":{
			"primary_window":{"used_percent":10,"limit_window_seconds":604800,"reset_at":1790000000},
			"secondary_window":{"used_percent":20}}}`
	})

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 2 {
		t.Fatalf("windows = %+v", report.Windows)
	}
	if w := report.Windows[0]; w.ID != "7d" || w.Label != "Weekly" || w.Duration != 7*24*time.Hour {
		t.Fatalf("weekly window = %+v", w)
	}
	// No length reported: the position is all that names the window.
	if w := report.Windows[1]; w.ID != "secondary" || w.Duration != 0 {
		t.Fatalf("window without a length = %+v", w)
	}
	if a := report.Account; a == nil || a.ID != "acc-1" || a.Email != "a@example.com" {
		t.Fatalf("account = %+v", a)
	}
}

func TestIDTokenEmail(t *testing.T) {
	for token, want := range map[string]string{
		idToken(`{"email":"a@example.com"}`): "a@example.com",
		idToken(`{"sub":"x"}`):               "",
		idToken(`not json`):                  "",
		"not-a-jwt":                          "",
		"":                                   "",
	} {
		if got := idTokenEmail(token); got != want {
			t.Errorf("idTokenEmail(%q) = %q, want %q", token, got, want)
		}
	}
	if a := (&authFile{}).account(); a != nil {
		t.Fatalf("a login with neither id nor email: account = %+v, want nil", a)
	}
}
