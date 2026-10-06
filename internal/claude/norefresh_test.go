package claude

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/McKean/aiquokka/internal/httpx/httpxtest"
	"github.com/McKean/aiquokka/internal/usage"
)

// writeHome puts files under a temporary HOME and keeps the Keychain out.
func writeHome(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for name, body := range files {
		path := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	original := readKeychainCredentials
	t.Cleanup(func() { readKeychainCredentials = original })
	readKeychainCredentials = func(context.Context) ([]byte, []byte, error) {
		return nil, nil, errKeychainCredentialsNotFound
	}
	return home
}

func TestFetchWithoutRefreshLeavesExpiredTokenAlone(t *testing.T) {
	stored := `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-refresh","expiresAt":1}}`
	home := writeHome(t, map[string]string{".claude/.credentials.json": stored})
	httpxtest.Refuse(t)

	_, err := Fetch(usage.WithoutRefresh(context.Background()))
	if !errors.Is(err, usage.ErrNoRefresh) {
		t.Fatalf("err = %v, want ErrNoRefresh", err)
	}
	after, readErr := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, []byte(stored)) {
		t.Fatalf("stored login was rewritten:\n%s", after)
	}
}

func TestLoadAccount(t *testing.T) {
	writeHome(t, map[string]string{
		".claude.json": `{"oauthAccount":{"accountUuid":"uuid-1","emailAddress":"a@example.com","organizationName":"x"},"other":1}`,
	})
	got := loadAccount()
	if got == nil || got.ID != "uuid-1" || got.Email != "a@example.com" {
		t.Fatalf("loadAccount() = %+v", got)
	}
}

func TestLoadAccountMissing(t *testing.T) {
	writeHome(t, nil)
	if got := loadAccount(); got != nil {
		t.Fatalf("no ~/.claude.json: loadAccount() = %+v, want nil", got)
	}
	writeHome(t, map[string]string{".claude.json": `{"numStartups":3}`})
	if got := loadAccount(); got != nil {
		t.Fatalf("no oauthAccount: loadAccount() = %+v, want nil", got)
	}
}

func TestWindowIDIsTheLimitKind(t *testing.T) {
	pct := 67.0
	resp := usageResponse{Limits: []limit{{Kind: "weekly_scoped", Percent: &pct}}}
	w := resp.window("weekly_scoped", "Weekly Fable", week)
	if w == nil || w.ID != "weekly_scoped" || w.Label != "Weekly Fable" || w.Duration != week {
		t.Fatalf("window = %+v", w)
	}
}
