package copilot

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/McKean/aiquokka/internal/httpx/httpxtest"
)

func TestFetchWindowIDsAndAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := filepath.Join(home, ".config", "github-copilot")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	apps := `{"github.com:Iv1.x":{"user":"octocat","oauth_token":"tok","githubAppId":"Iv1.x"}}`
	if err := os.WriteFile(filepath.Join(dir, "apps.json"), []byte(apps), 0o600); err != nil {
		t.Fatal(err)
	}
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		if got := r.Header.Get("Authorization"); got != "token tok" {
			t.Errorf("Authorization = %q", got)
		}
		return http.StatusOK, `{"copilot_plan":"individual","quota_reset_date_utc":"2026-11-01T00:00:00Z","quota_snapshots":{
			"premium_interactions":{"percent_remaining":75},"chat":{"percent_remaining":100}}}`
	})

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 2 || report.Windows[0].ID != "premium_interactions" || report.Windows[1].ID != "chat" {
		t.Fatalf("windows = %+v", report.Windows)
	}
	if report.Windows[0].Label != "Premium" || report.Windows[0].Duration <= 0 {
		t.Fatalf("premium window = %+v", report.Windows[0])
	}
	if a := report.Account; a == nil || a.ID != "octocat" || a.Email != "" {
		t.Fatalf("account = %+v", a)
	}
}
