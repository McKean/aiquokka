package muse

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

func TestReportFromMintWindows(t *testing.T) {
	resp, err := parseMint([]byte(`{
		"is_subs_active": true,
		"require_payment": false,
		"subs_tier_name": "Muse Code Power Usage",
		"api_key": "LLM|discarded",
		"subs_usage": {
			"window": {"used_percent": 24, "window_duration_mins": 300, "resets_at": 1789568145},
			"weekly": {"used_percent": 45.5, "resets_at": 1789948800}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	report := reportFromMint(resp)

	if report.Provider != "Muse" {
		t.Fatalf("Provider = %q, want Muse", report.Provider)
	}
	if report.Plan != "Muse Code Power Usage" {
		t.Fatalf("Plan = %q, want the tier name", report.Plan)
	}
	if len(report.Windows) != 2 {
		t.Fatalf("Windows = %d, want 2", len(report.Windows))
	}
	five := report.Windows[0]
	if five.ID != "5h" || five.Label != "5h" {
		t.Fatalf("rolling window id/label = %q/%q, want 5h/5h", five.ID, five.Label)
	}
	if five.UsedPercent == nil || *five.UsedPercent != 24 {
		t.Fatalf("rolling used = %v, want 24", five.UsedPercent)
	}
	if five.Duration != 5*time.Hour {
		t.Fatalf("rolling duration = %v, want 5h", five.Duration)
	}
	if want := time.Unix(1789568145, 0).UTC(); !five.ResetsAt.Equal(want) {
		t.Fatalf("rolling resets = %v, want %v", five.ResetsAt, want)
	}
	weekly := report.Windows[1]
	if weekly.ID != "weekly" || weekly.Label != "Weekly" {
		t.Fatalf("weekly id/label = %q/%q, want weekly/Weekly", weekly.ID, weekly.Label)
	}
	if weekly.UsedPercent == nil || *weekly.UsedPercent != 45.5 {
		t.Fatalf("weekly used = %v, want 45.5", weekly.UsedPercent)
	}
	if weekly.Duration != 7*24*time.Hour {
		t.Fatalf("weekly duration = %v, want 168h", weekly.Duration)
	}
}

func TestReportFromMintIdleKeepsPlan(t *testing.T) {
	// Meta omits subs_usage while the 5-hour window is idle: the plan stays,
	// and no window is invented.
	resp, err := parseMint([]byte(`{
		"is_subs_active": true,
		"subs_tier_name": "Muse Code Power Usage"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	report := reportFromMint(resp)
	if report.Plan != "Muse Code Power Usage" {
		t.Fatalf("Plan = %q, want the tier name", report.Plan)
	}
	if len(report.Windows) != 0 {
		t.Fatalf("Windows = %d, want 0 (idle must not fake a 0%%)", len(report.Windows))
	}
	if len(report.Extra) != 1 || report.Extra[0].Label != "Usage" {
		t.Fatalf("Extra = %+v, want one Usage fact", report.Extra)
	}
}

func TestReportFromMintInactive(t *testing.T) {
	resp, err := parseMint([]byte(`{
		"is_subs_active": false,
		"subs_usage": {"weekly": {"used_percent": 10, "resets_at": 1789948800}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	report := reportFromMint(resp)
	if len(report.Windows) != 0 {
		t.Fatalf("Windows = %d, want 0 for an inactive subscription", len(report.Windows))
	}
	if len(report.Extra) != 1 || report.Extra[0].Value != "inactive" {
		t.Fatalf("Extra = %+v, want Subscription inactive", report.Extra)
	}
}

func TestReportFromMintBillingIncomplete(t *testing.T) {
	resp, err := parseMint([]byte(`{"is_subs_active": true, "require_payment": true}`))
	if err != nil {
		t.Fatal(err)
	}
	report := reportFromMint(resp)
	if len(report.Extra) != 1 || report.Extra[0].Value != "billing incomplete" {
		t.Fatalf("Extra = %+v, want Subscription billing incomplete", report.Extra)
	}
}

func TestReportFromMintSkipsMalformedWindows(t *testing.T) {
	resp, err := parseMint([]byte(`{
		"is_subs_active": true,
		"subs_usage": {
			"window": {"used_percent": "nope", "resets_at": 1789568145},
			"weekly": {"used_percent": 12, "resets_at": "not a time"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	report := reportFromMint(resp)
	if len(report.Windows) != 1 || report.Windows[0].ID != "weekly" {
		t.Fatalf("Windows = %+v, want only the weekly window", report.Windows)
	}
	if !report.Windows[0].ResetsAt.IsZero() {
		t.Fatalf("weekly resets = %v, want unknown", report.Windows[0].ResetsAt)
	}
	if report.Windows[0].UsedPercent == nil || *report.Windows[0].UsedPercent != 12 {
		t.Fatalf("weekly used = %v, want 12", report.Windows[0].UsedPercent)
	}
}

func TestResetsAtFormats(t *testing.T) {
	want := time.Unix(1789568145, 0).UTC()
	cases := map[string]any{
		"unix seconds":     float64(1789568145),
		"unix millis":      float64(1789568145000),
		"string seconds":   "1789568145",
		"rfc3339":          "2026-09-16T14:15:45Z",
		"rfc3339 fraction": "2026-09-16T14:15:45.123Z",
	}
	for name, v := range cases {
		got := resetsAt(v)
		if strings.HasPrefix(name, "rfc3339") {
			if got.IsZero() || got.Unix() != 1789568145 {
				t.Errorf("%s: got %v, want unix 1789568145", name, got)
			}
			continue
		}
		if !got.Equal(want) {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	for _, v := range []any{nil, "garbage", "", -5.0} {
		if got := resetsAt(v); !got.IsZero() {
			t.Errorf("resetsAt(%v) = %v, want zero", v, got)
		}
	}
}

func writeAuth(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUSE_AUTH_PATH", path)
	return path
}

func TestLoadTokenInline(t *testing.T) {
	writeAuth(t, `{"providers":{"meta":{"mechanism":"oauth","access_token":"dca:test-token"}}}`)
	got, err := loadToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "dca:test-token" {
		t.Fatalf("token = %q, want the inline access token", got)
	}
}

func TestLoadTokenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("MUSE_AUTH_PATH", path)
	if _, err := loadToken(context.Background()); !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want NotConfigured", err)
	}
}

func TestLoadTokenNoMetaLogin(t *testing.T) {
	writeAuth(t, `{"providers":{"meta":{"mechanism":"api_key"}}}`)
	if _, err := loadToken(context.Background()); !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want NotConfigured", err)
	}
}

func TestKeychainAccessToken(t *testing.T) {
	got, err := keychainAccessToken([]byte(`{"access_token":"dca:from-keychain"}`))
	if err != nil || got != "dca:from-keychain" {
		t.Fatalf("got %q, %v; want the JSON token", got, err)
	}
	got, err = keychainAccessToken([]byte("dca:bare-token"))
	if err != nil || got != "dca:bare-token" {
		t.Fatalf("got %q, %v; want the bare token", got, err)
	}
	for _, payload := range []string{"", "{}", `{"access_token":""}`} {
		if _, err := keychainAccessToken([]byte(payload)); !isKeychainNotFound(err) {
			t.Fatalf("payload %q: err = %v, want keychain-not-found", payload, err)
		}
	}
}

func stubKeychainLoader(t *testing.T, fn func(context.Context) (string, error)) {
	t.Helper()
	old := keychainLoader
	keychainLoader = fn
	t.Cleanup(func() { keychainLoader = old })
}

func TestClassifyKeychainFailure(t *testing.T) {
	runErr := errKeychainNotFound
	cases := []struct {
		name       string
		timedOut   bool
		code       int
		hasCode    bool
		stderr     string
		unreadable bool
	}{
		{"timeout beats everything", true, 44, true, "could not be found", true},
		{"exit 44 is missing", false, 44, true, "", false},
		{"not found text is missing", false, 1, true, "The specified item could not be found in the keychain.", false},
		{"locked keychain", false, 1, true, "The user name or passphrase you entered is not correct because it is locked.", true},
		{"no user interaction", false, 1, true, "User interaction is not allowed.", true},
		{"no dialogue", false, 1, true, "Unable to display a dialogue.", true},
		{"other security error", false, 1, true, "security: some new failure", true},
		{"multiline security error", false, 1, true, "security: denied\nrequest was not authorized", true},
		{"empty failure", false, 1, true, "", true},
	}
	for _, tc := range cases {
		err := classifyKeychainFailure(tc.timedOut, tc.code, tc.hasCode, tc.stderr, runErr)
		if !isKeychainNotFound(err) {
			t.Errorf("%s: err = %v, want keychain-not-found underneath", tc.name, err)
		}
		if _, ok := asKeychainUnreadable(err); ok != tc.unreadable {
			t.Errorf("%s: unreadable = %v, want %v (err = %v)", tc.name, ok, tc.unreadable, err)
		}
		if tc.name == "multiline security error" {
			if unread, _ := asKeychainUnreadable(err); strings.Contains(unread.reason, "\n") {
				t.Errorf("reason = %q, want one line", unread.reason)
			}
		}
	}
}

func TestLoadTokenKeychainTimeout(t *testing.T) {
	writeAuth(t, `{"providers":{"meta":{"mechanism":"oauth"}}}`)
	stubKeychainLoader(t, func(context.Context) (string, error) {
		return "", classifyKeychainFailure(true, 0, false, "", context.DeadlineExceeded)
	})
	_, err := loadToken(context.Background())
	if !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want NotConfigured", err)
	}
	msg := err.Error()
	// Both commands are spelled out: the dialog aiquokka's own bounded read
	// raised cannot grant access, so the message must name ones that can.
	for _, want := range []string{
		"ai.meta.dev.credentials", "could not be read", "dismiss it",
		"`security find-generic-password -s ai.meta.dev.credentials -a meta -w >/dev/null`",
		"Always Allow",
		"`security set-generic-password-partition-list -S apple-tool:,apple:,teamid:V9WTTPBFK9 -s ai.meta.dev.credentials -a meta`",
		"security unlock-keychain",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("err = %q, want it to mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "answer the permission dialog") {
		t.Errorf("err = %q, must not send the user to the dialog aiquokka raised", msg)
	}
	if strings.Contains(msg, "run `muse login`") {
		t.Errorf("err = %q, must not send the user back to `muse login`", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("err = %q, want one line", msg)
	}
}

func TestLoadTokenKeychainMissing(t *testing.T) {
	path := writeAuth(t, `{"providers":{"meta":{"mechanism":"oauth"}}}`)
	stubKeychainLoader(t, func(context.Context) (string, error) {
		return "", classifyKeychainFailure(false, 44, true, "could not be found", errKeychainNotFound)
	})
	_, err := loadToken(context.Background())
	if !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want NotConfigured", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "run `muse login`") || !strings.Contains(msg, path) {
		t.Errorf("err = %q, want today's login hint with the auth path", msg)
	}
}

func TestLoadTokenKeychainLocked(t *testing.T) {
	writeAuth(t, `{"providers":{"meta":{"mechanism":"oauth"}}}`)
	stubKeychainLoader(t, func(context.Context) (string, error) {
		return "", classifyKeychainFailure(false, 1, true, "User interaction is not allowed.", errKeychainNotFound)
	})
	_, err := loadToken(context.Background())
	if !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want NotConfigured", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "security unlock-keychain") {
		t.Errorf("err = %q, want the unlock hint", msg)
	}
}

// mintServer serves one canned mint response, recording the request it saw.
type mintServer struct {
	t        *testing.T
	hits     int
	auth     string
	versions []string
	body     []byte
	status   int
	payload  string
}

func (m *mintServer) handler(w http.ResponseWriter, r *http.Request) {
	m.hits++
	if r.Method != http.MethodPost {
		m.t.Errorf("method = %s, want POST", r.Method)
	}
	m.auth = r.Header.Get("Authorization")
	m.versions = append(m.versions, r.Header.Get("X-Api-Version"))
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		m.t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body, _ := io.ReadAll(r.Body)
	m.body = body
	w.WriteHeader(m.status)
	_, _ = w.Write([]byte(m.payload))
}

func serveMint(t *testing.T, status int, payload string) *mintServer {
	t.Helper()
	m := &mintServer{t: t, status: status, payload: payload}
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(srv.Close)
	old := keyEndpoint
	keyEndpoint = srv.URL
	t.Cleanup(func() { keyEndpoint = old })
	resetMintCache()
	t.Cleanup(resetMintCache)
	return m
}

const mintPayload = `{
	"is_subs_active": true,
	"subs_tier_name": "Muse Code Power Usage",
	"subs_usage": {
		"window": {"used_percent": 10, "window_duration_mins": 300, "resets_at": 1789568145},
		"weekly": {"used_percent": 20, "resets_at": 1789948800}
	}
}`

func TestFetchMintsWithDeviceToken(t *testing.T) {
	writeAuth(t, `{"providers":{"meta":{"mechanism":"oauth","access_token":"dca:test-token"}}}`)
	m := serveMint(t, http.StatusOK, mintPayload)

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if m.hits != 1 {
		t.Fatalf("endpoint hits = %d, want 1", m.hits)
	}
	if m.auth != "Bearer dca:test-token" {
		t.Fatalf("Authorization = %q, want the device token", m.auth)
	}
	if len(m.versions) != 1 || m.versions[0] != "1.0.0" {
		t.Fatalf("X-Api-Version = %v, want 1.0.0", m.versions)
	}
	if string(m.body) != "{}" {
		t.Fatalf("body = %q, want {}", m.body)
	}
	if len(report.Windows) != 2 || report.Plan == "" {
		t.Fatalf("report = %+v, want plan and two windows", report)
	}
}

func TestFetchReusesMintWithinTTL(t *testing.T) {
	writeAuth(t, `{"providers":{"meta":{"mechanism":"oauth","access_token":"dca:test-token"}}}`)
	m := serveMint(t, http.StatusOK, mintPayload)

	if _, err := Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.hits != 1 {
		t.Fatalf("endpoint hits = %d, want 1 (cached)", m.hits)
	}
}

func TestFetchRejectedLogin(t *testing.T) {
	writeAuth(t, `{"providers":{"meta":{"mechanism":"oauth","access_token":"LLM|not-a-login"}}}`)
	serveMint(t, http.StatusUnauthorized, `{"error":"bad key","email":"you@example.com"}`)

	_, err := Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch succeeded, want rejection")
	}
	if !strings.Contains(err.Error(), "muse login") {
		t.Fatalf("err = %q, want a re-login hint", err)
	}
	if strings.Contains(err.Error(), "you@example.com") {
		t.Fatalf("err = %q, must not leak the response body", err)
	}
}

func TestFetchNotConfigured(t *testing.T) {
	t.Setenv("MUSE_AUTH_PATH", filepath.Join(t.TempDir(), "auth.json"))
	resetMintCache()
	t.Cleanup(resetMintCache)
	if _, err := Fetch(context.Background()); !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want NotConfigured", err)
	}
}
