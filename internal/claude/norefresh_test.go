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
	t.Setenv("CLAUDE_CONFIG_DIR", "")
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

func TestClaudeConfigDirIsHonoured(t *testing.T) {
	home := writeHome(t, map[string]string{
		".claude/.credentials.json": `{"claudeAiOauth":{"accessToken":"home-token"}}`,
		".claude.json":              `{"oauthAccount":{"accountUuid":"home-uuid","emailAddress":"home@example.com"}}`,
	})
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"dir-token"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount":{"accountUuid":"dir-uuid","emailAddress":"dir@example.com"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	o, err := loadCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if o.AccessToken != "dir-token" {
		t.Fatalf("AccessToken = %q, want dir-token", o.AccessToken)
	}
	got := loadAccount()
	if got == nil || got.ID != "dir-uuid" || got.Email != "dir@example.com" {
		t.Fatalf("loadAccount() = %+v, want the account in CLAUDE_CONFIG_DIR", got)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", "")
	o, err = loadCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if o.AccessToken != "home-token" {
		t.Fatalf("empty CLAUDE_CONFIG_DIR: AccessToken = %q, want home-token", o.AccessToken)
	}
	got = loadAccount()
	if got == nil || got.Email != "home@example.com" {
		t.Fatalf("empty CLAUDE_CONFIG_DIR: loadAccount() = %+v, want the account in %s", got, home)
	}
}

func TestFetchWithoutRefreshLeavesConfigDirTokenAlone(t *testing.T) {
	writeHome(t, nil)
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	stored := `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-refresh","expiresAt":1}}`
	path := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}
	httpxtest.Refuse(t)

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

func TestWindowIDIsTheLimitKind(t *testing.T) {
	pct := 67.0
	resp := usageResponse{Limits: []limit{{Kind: "weekly_scoped", Percent: &pct}}}
	w := resp.window("weekly_scoped", "Weekly Fable", week)
	if w == nil || w.ID != "weekly_scoped" || w.Label != "Weekly Fable" || w.Duration != week {
		t.Fatalf("window = %+v", w)
	}
	if w.Model != "Fable" {
		t.Fatalf("weekly_scoped Model = %q, want Fable", w.Model)
	}
	// The other windows count for every model, so they name none.
	for _, kind := range []string{"session", "weekly_all"} {
		shared := usageResponse{Limits: []limit{{Kind: kind, Percent: &pct}}}
		w := shared.window(kind, kind, week)
		if w == nil {
			t.Fatalf("%s window is nil", kind)
		}
		if w.Model != "" {
			t.Fatalf("%s Model = %q, want none", kind, w.Model)
		}
	}
}
