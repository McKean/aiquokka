package antigravity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/httpx/httpxtest"
	"github.com/McKean/aiquokka/internal/usage"
)

func stubKeyring(t *testing.T, r func(context.Context) ([]byte, error), w func(context.Context, []byte) error) {
	t.Helper()
	origRead, origWrite := readKeyring, writeKeyring
	t.Cleanup(func() {
		readKeyring = origRead
		writeKeyring = origWrite
	})
	if r != nil {
		readKeyring = r
	} else {
		readKeyring = func(context.Context) ([]byte, error) { return nil, errKeyringNotFound }
	}
	if w != nil {
		writeKeyring = w
	} else {
		writeKeyring = func(context.Context, []byte) error { return errKeyringNotFound }
	}
}

func writeTokenFile(t *testing.T, body string) string {
	t.Helper()
	stubKeyring(t, nil, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "antigravity-oauth-token")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tokenEnvVar, path)
	return path
}

func stubClientEnv(t *testing.T) (clientID, clientSecret string) {
	t.Helper()
	clientID = "123456789012-testclient123456.apps.googleusercontent.com"
	clientSecret = clientSecretPfx + strings.Repeat("a", clientSecretTailN)
	t.Setenv(clientIDEnvVar, clientID)
	t.Setenv(clientSecretEnvVar, clientSecret)
	return clientID, clientSecret
}

func makeIDToken(email string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(`{"email":"`+email+`"}`)) + ".sig"
}

func TestParseQuotaSummaryWindowIDsAndLengths(t *testing.T) {
	body := []byte(`{
		"groups": [
			{
				"displayName": "Gemini Models",
				"buckets": [
					{
						"bucketId": "gemini-weekly",
						"displayName": "Weekly Limit Remaining",
						"window": "weekly",
						"remainingFraction": 0.93,
						"resetTime": "2026-11-19T18:29:53Z"
					},
					{
						"bucketId": "gemini-5h",
						"displayName": "Five Hour Limit Remaining",
						"window": "5h",
						"remainingFraction": 0.85,
						"resetTime": "2026-11-13T09:06:05Z"
					}
				]
			},
			{
				"displayName": "Gemini 4 Argon",
				"buckets": [
					{
						"bucketId": "gemini-argon-wtu-weekly",
						"displayName": "Gemini 4 Argon Weekly Limit",
						"window": "weekly",
						"remainingFraction": 0,
						"resetTime": "2026-11-14T13:03:20Z"
					},
					{
						"bucketId": "extra-daily",
						"displayName": "Daily Limit Remaining",
						"window": "daily",
						"remainingFraction": 0.5
					}
				]
			}
		]
	}`)

	report, err := parseQuotaSummary(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 4 {
		t.Fatalf("got %d windows, want 4", len(report.Windows))
	}

	w0 := report.Windows[0]
	if w0.ID != "gemini-models/weekly-limit-remaining" {
		t.Errorf("w0.ID = %q", w0.ID)
	}
	if w0.Label != "Gemini Models - Weekly" {
		t.Errorf("w0.Label = %q", w0.Label)
	}
	if w0.Duration != 7*24*time.Hour {
		t.Errorf("w0.Duration = %v", w0.Duration)
	}
	if w0.UsedPercent == nil || *w0.UsedPercent < 6.9 || *w0.UsedPercent > 7.1 {
		t.Errorf("w0.UsedPercent = %v", w0.UsedPercent)
	}

	w1 := report.Windows[1]
	if w1.ID != "gemini-models/five-hour-limit-remaining" {
		t.Errorf("w1.ID = %q", w1.ID)
	}
	if w1.Label != "Gemini Models - 5h" {
		t.Errorf("w1.Label = %q", w1.Label)
	}
	if w1.Duration != 5*time.Hour {
		t.Errorf("w1.Duration = %v", w1.Duration)
	}

	w2 := report.Windows[2]
	if w2.UsedPercent == nil || *w2.UsedPercent != 100 {
		t.Errorf("w2.UsedPercent = %v, want 100", w2.UsedPercent)
	}

	w3 := report.Windows[3]
	if w3.Label != "Gemini 4 Argon - Daily Limit" || w3.Duration != 0 {
		t.Errorf("w3 = %+v", w3)
	}
}

func TestParseQuotaSummarySkipsDisabledBuckets(t *testing.T) {
	body := []byte(`{
		"groups": [{
			"displayName": "Gemini Models",
			"buckets": [
				{
					"bucketId": "gemini-weekly",
					"displayName": "Weekly Limit Remaining",
					"window": "weekly",
					"remainingFraction": 0.4
				},
				{
					"bucketId": "gemini-5h",
					"displayName": "Five Hour Limit Remaining",
					"window": "5h",
					"remainingFraction": 1,
					"disabled": true
				}
			]
		}]
	}`)

	report, err := parseQuotaSummary(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 1 {
		t.Fatalf("got %d windows, want 1 (disabled 5h bucket must be omitted): %+v", len(report.Windows), report.Windows)
	}
	if report.Windows[0].ID != "gemini-models/weekly-limit-remaining" {
		t.Errorf("window ID = %q", report.Windows[0].ID)
	}
}

func TestParseQuotaSummaryAllDisabledReportsFact(t *testing.T) {
	body := []byte(`{
		"groups": [{
			"displayName": "Gemini Models",
			"buckets": [{
				"bucketId": "gemini-5h",
				"displayName": "Five Hour Limit Remaining",
				"window": "5h",
				"remainingFraction": 1,
				"disabled": true
			}]
		}]
	}`)

	report, err := parseQuotaSummary(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 0 {
		t.Fatalf("Windows = %+v, want 0", report.Windows)
	}
	if len(report.Extra) != 1 || report.Extra[0].Label != "Usage" {
		t.Fatalf("Extra = %+v, want disabled usage fact", report.Extra)
	}
}

func TestDecodeKeyringSecret(t *testing.T) {
	want := []byte(`{"token":{"access_token":"abc"}}`)

	if got := decodeKeyringSecret(want); !bytes.Equal(got, want) {
		t.Errorf("raw = %q", got)
	}
	b64 := []byte(goKeyringBase64Prefix + base64.StdEncoding.EncodeToString(want))
	if got := decodeKeyringSecret(b64); !bytes.Equal(got, want) {
		t.Errorf("base64 = %q", got)
	}
	hexWrapped := []byte(hex.EncodeToString(b64))
	if got := decodeKeyringSecret(hexWrapped); !bytes.Equal(got, want) {
		t.Errorf("hex+base64 = %q", got)
	}
}

func TestFetchKeyringOnlyLoginAndRefreshPersist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(tokenEnvVar, "")
	stubClientEnv(t)

	expired := `{
		"token": {"access_token": "old-kr", "refresh_token": "kr-refresh", "expiry": "2020-01-01T00:00:00Z", "keep": 1},
		"auth_method": "consumer",
		"id_token_email": "kr@example.com"
	}`
	var written []byte
	stubKeyring(t,
		func(context.Context) ([]byte, error) { return []byte(expired), nil },
		func(_ context.Context, data []byte) error {
			written = append([]byte(nil), data...)
			return nil
		},
	)

	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		switch {
		case r.URL.Host == "oauth2.googleapis.com":
			return http.StatusOK, `{"access_token":"new-kr","expires_in":3599,"token_type":"Bearer"}`
		case r.URL.Path == "/v1internal:loadCodeAssist":
			if got := r.Header.Get("Authorization"); got != "Bearer new-kr" {
				t.Errorf("Authorization = %q, want Bearer new-kr", got)
			}
			return http.StatusOK, `{"cloudaicompanionProject":"aicode-consumers","paidTier":{"name":"Google AI Pro"}}`
		case r.URL.Path == "/v1internal:retrieveUserQuotaSummary":
			return http.StatusOK, `{"groups":[{"displayName":"Gemini Models","buckets":[{"displayName":"Weekly Limit Remaining","window":"weekly","remainingFraction":0.75}]}]}`
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
			return 0, ""
		}
	})

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Plan != "Google AI Pro" || report.Account == nil || report.Account.Email != "kr@example.com" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(written) == 0 || bytes.Contains(written, []byte("\n")) {
		t.Fatalf("expected compact single-line JSON written to keyring, got: %q", written)
	}
	var saved map[string]any
	if err := json.Unmarshal(written, &saved); err != nil {
		t.Fatal(err)
	}
	tok, _ := saved["token"].(map[string]any)
	if tok["access_token"] != "new-kr" || tok["keep"] != float64(1) {
		t.Errorf("keyring saved token = %#v", tok)
	}
}

func TestFetchFallsBackToFileWhenKeyringLockedAndPrefersFresher(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(tokenEnvVar, "")

	tokPath := filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(tokPath), 0o700); err != nil {
		t.Fatal(err)
	}
	newer := time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano)
	older := time.Now().Add(10 * time.Minute).Format(time.RFC3339Nano)

	if err := os.WriteFile(tokPath, []byte(`{
		"token": {"access_token": "file-access", "refresh_token": "ref", "expiry": "`+newer+`"},
		"id_token_email": "file@example.com"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Case 1: keyring is locked -> falls back cleanly to file.
	stubKeyring(t, func(context.Context) ([]byte, error) { return nil, errKeyringLocked }, nil)
	auth, err := loadAuth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if auth.Token.AccessToken != "file-access" || auth.source != sourceFile {
		t.Errorf("locked fallback got %+v", auth)
	}

	// Case 2: keyring has older token, file has newer -> chooses file.
	stubKeyring(t, func(context.Context) ([]byte, error) {
		return []byte(`{"token":{"access_token":"kr-older","refresh_token":"ref","expiry":"` + older + `"}}`), nil
	}, nil)
	auth, err = loadAuth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if auth.Token.AccessToken != "file-access" || auth.source != sourceFile {
		t.Errorf("fresher selection got %+v", auth)
	}
}

func TestFetchKeyringLockedWithoutFileSurfacesError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(tokenEnvVar, "")
	stubKeyring(t, func(context.Context) ([]byte, error) { return nil, errKeyringLocked }, nil)
	httpxtest.Refuse(t)

	_, err := Fetch(context.Background())
	if !errors.Is(err, errKeyringLocked) {
		t.Fatalf("err = %v, want errKeyringLocked", err)
	}
}

func TestFetchLoadsTierQuotaAndAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(tokenEnvVar, "")
	stubKeyring(t, nil, nil)

	tokPath := filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(tokPath), 0o700); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	err := os.WriteFile(tokPath, []byte(`{
		"token": {"access_token": "valid-access", "refresh_token": "ref", "expiry": "`+future+`"},
		"auth_method": "consumer",
		"id_token_email": "user@example.com"
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		if got := r.Header.Get("User-Agent"); got != apiUserAgent {
			t.Errorf("User-Agent = %q, want %q", got, apiUserAgent)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer valid-access" {
			t.Errorf("Authorization = %q", got)
		}
		rawBody, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			if !strings.Contains(string(rawBody), `"ANTIGRAVITY"`) {
				t.Errorf("loadCodeAssist body = %s", rawBody)
			}
			return http.StatusOK, `{
				"cloudaicompanionProject": "aicode-consumers",
				"currentTier": {"id": "free-tier", "name": "Antigravity"},
				"paidTier": {"id": "g1-pro-tier", "name": "Google AI Pro"}
			}`
		case "/v1internal:retrieveUserQuotaSummary":
			if !strings.Contains(string(rawBody), `"aicode-consumers"`) {
				t.Errorf("retrieveUserQuotaSummary body = %s", rawBody)
			}
			return http.StatusOK, `{
				"groups": [{
					"displayName": "Gemini Models",
					"buckets": [{"displayName": "Weekly Limit Remaining", "window": "weekly", "remainingFraction": 0.8}]
				}]
			}`
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
			return 0, ""
		}
	})

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Plan != "Google AI Pro" {
		t.Errorf("Plan = %q, want Google AI Pro", report.Plan)
	}
	if report.Account == nil || report.Account.Email != "user@example.com" {
		t.Errorf("Account = %+v", report.Account)
	}
	if len(report.Windows) != 1 {
		t.Fatalf("Windows = %+v", report.Windows)
	}
}

func TestFetchRefreshesExpiringTokenAndPersists(t *testing.T) {
	testClientID, _ := stubClientEnv(t)
	soon := time.Now().Add(10 * time.Second).Format(time.RFC3339Nano)
	path := writeTokenFile(t, `{
  "token": {
    "access_token": "old-access",
    "refresh_token": "old-refresh",
    "token_type": "Bearer",
    "expiry": "`+soon+`",
    "custom_token_field": 42
  },
  "auth_method": "consumer",
  "custom_top": "keep-me"
}`)

	var refreshCalls int
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		switch r.URL.Host {
		case "oauth2.googleapis.com":
			refreshCalls++
			formBytes, _ := io.ReadAll(r.Body)
			q := string(formBytes)
			if !strings.Contains(q, "refresh_token=old-refresh") || !strings.Contains(q, "client_id="+testClientID) {
				t.Errorf("unexpected refresh body: %s", q)
			}
			return http.StatusOK, `{
				"access_token": "new-access",
				"refresh_token": "new-refresh",
				"expires_in": 3599,
				"token_type": "Bearer",
				"id_token": "` + makeIDToken("refreshed@example.com") + `"
			}`
		case "daily-cloudcode-pa.googleapis.com":
			if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
				t.Errorf("Authorization = %q, want Bearer new-access", got)
			}
			if r.URL.Path == "/v1internal:loadCodeAssist" {
				return http.StatusOK, `{"cloudaicompanionProject":"aicode-consumers","currentTier":{"name":"Antigravity"}}`
			}
			return http.StatusOK, `{"groups":[{"displayName":"Gemini Models","buckets":[{"displayName":"Weekly Limit Remaining","window":"weekly","remainingFraction":0.9}]}]}`
		default:
			t.Fatalf("unexpected URL: %s", r.URL.String())
			return 0, ""
		}
	})

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if refreshCalls != 1 {
		t.Errorf("refreshCalls = %d, want 1", refreshCalls)
	}
	if report.Plan != "Antigravity" {
		t.Errorf("Plan = %q, want Antigravity", report.Plan)
	}
	if report.Account == nil || report.Account.Email != "refreshed@example.com" {
		t.Errorf("Account = %+v", report.Account)
	}

	rawAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(rawAfter, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["custom_top"] != "keep-me" || saved["auth_method"] != "consumer" {
		t.Errorf("top-level keys lost: %s", rawAfter)
	}
	tok, _ := saved["token"].(map[string]any)
	if tok["access_token"] != "new-access" || tok["refresh_token"] != "new-refresh" || tok["custom_token_field"] != float64(42) {
		t.Errorf("token map = %#v", tok)
	}
}

func TestFetch401TriggersRefreshAndRetry(t *testing.T) {
	stubClientEnv(t)
	future := time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	writeTokenFile(t, `{
		"token": {"access_token": "stale-access", "refresh_token": "ref", "expiry": "`+future+`"},
		"id_token_email": "user@example.com"
	}`)

	var quotaCalls int
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		switch {
		case r.URL.Host == "oauth2.googleapis.com":
			return http.StatusOK, `{"access_token":"fresh-access","expires_in":3599,"token_type":"Bearer"}`
		case r.URL.Path == "/v1internal:loadCodeAssist":
			return http.StatusOK, `{"cloudaicompanionProject":"aicode-consumers","paidTier":{"name":"Google AI Pro"}}`
		case r.URL.Path == "/v1internal:retrieveUserQuotaSummary":
			quotaCalls++
			if quotaCalls == 1 {
				if r.Header.Get("Authorization") != "Bearer stale-access" {
					t.Errorf("first quota auth = %q", r.Header.Get("Authorization"))
				}
				return http.StatusUnauthorized, `{"error":{"message":"invalid credentials"}}`
			}
			if r.Header.Get("Authorization") != "Bearer fresh-access" {
				t.Errorf("retry quota auth = %q", r.Header.Get("Authorization"))
			}
			return http.StatusOK, `{"groups":[{"displayName":"Gemini Models","buckets":[{"displayName":"Weekly Limit Remaining","window":"weekly","remainingFraction":1}]}]}`
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
			return 0, ""
		}
	})

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if quotaCalls != 2 {
		t.Errorf("quotaCalls = %d, want 2", quotaCalls)
	}
	if len(report.Windows) != 1 || *report.Windows[0].UsedPercent != 0 {
		t.Errorf("Windows = %+v", report.Windows)
	}
}

func TestFetchNoRefreshLeavesExpiredAndRejectedTokensAlone(t *testing.T) {
	expired := `{
		"token": {"access_token": "old", "refresh_token": "ref", "expiry": "2020-01-01T00:00:00Z"},
		"id_token_email": "user@example.com"
	}`
	path := writeTokenFile(t, expired)
	httpxtest.Refuse(t)

	_, err := Fetch(usage.WithoutRefresh(context.Background()))
	if !errors.Is(err, usage.ErrNoRefresh) {
		t.Fatalf("expired err = %v, want ErrNoRefresh", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, []byte(expired)) {
		t.Fatalf("expired token file modified:\n%s", after)
	}

	future := time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	rejected := `{"token":{"access_token":"revoked","refresh_token":"ref","expiry":"` + future + `"}}`
	path = writeTokenFile(t, rejected)
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		if r.URL.Host == "oauth2.googleapis.com" {
			t.Fatalf("unexpected refresh call under --no-refresh")
		}
		return http.StatusUnauthorized, `{}`
	})

	_, err = Fetch(usage.WithoutRefresh(context.Background()))
	if !errors.Is(err, usage.ErrNoRefresh) {
		t.Fatalf("401 err = %v, want ErrNoRefresh", err)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(after, []byte(rejected)) {
		t.Fatalf("rejected token file modified:\n%s", after)
	}
}

func TestFetchMissingTokenIsNotConfigured(t *testing.T) {
	stubKeyring(t, nil, nil)
	t.Setenv(tokenEnvVar, filepath.Join(t.TempDir(), "missing"))
	httpxtest.Refuse(t)

	_, err := Fetch(context.Background())
	if !usage.IsNotConfigured(err) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestExtractAndCacheOAuthClientFromBinary(t *testing.T) {
	t.Setenv(clientIDEnvVar, "")
	t.Setenv(clientSecretEnvVar, "")

	dir := t.TempDir()
	binPath := filepath.Join(dir, "agy")
	cachePath := filepath.Join(dir, "client-cache.json")

	origFind, origCache := findAgyBinaryFn, clientCachePathFn
	t.Cleanup(func() {
		findAgyBinaryFn = origFind
		clientCachePathFn = origCache
	})
	findAgyBinaryFn = func() (string, error) { return binPath, nil }
	clientCachePathFn = func() (string, error) { return cachePath, nil }

	badID := "111111111111-badclient0000000" + clientIDDomain
	goodID := "222222222222-goodclient111111" + clientIDDomain
	badSec := clientSecretPfx + strings.Repeat("b", clientSecretTailN)
	goodSec := clientSecretPfx + strings.Repeat("g", clientSecretTailN)

	// Embed strings across a chunk boundary with concatenated Go .rodata
	// lowercase prefixes and suffixes (no NUL terminators).
	var payload bytes.Buffer
	payload.WriteString("noise:" + badID + "tailstring")
	payload.Write(bytes.Repeat([]byte("X"), scanChunkSize-payload.Len()-10))
	payload.WriteString("deprecated_as_an_error" + goodID + "other")
	payload.WriteString(badSec + "https://example.com")
	payload.WriteString(goodSec + "decodingError")
	if err := os.WriteFile(binPath, payload.Bytes(), 0o755); err != nil {
		t.Fatal(err)
	}

	writeTokenFile(t, `{"token":{"access_token":"old","refresh_token":"ref","expiry":"2020-01-01T00:00:00Z"}}`)

	var triedPairs []string
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		switch {
		case r.URL.Host == "oauth2.googleapis.com":
			raw, _ := io.ReadAll(r.Body)
			q := string(raw)
			triedPairs = append(triedPairs, q)
			if strings.Contains(q, goodID) && strings.Contains(q, goodSec) {
				return http.StatusOK, `{"access_token":"fresh-access","expires_in":3599,"token_type":"Bearer"}`
			}
			return http.StatusUnauthorized, `{"error":"invalid_client","error_description":"The provided client secret is invalid."}`
		case r.URL.Path == "/v1internal:loadCodeAssist":
			return http.StatusOK, `{"cloudaicompanionProject":"aicode-consumers","paidTier":{"name":"Google AI Pro"}}`
		case r.URL.Path == "/v1internal:retrieveUserQuotaSummary":
			return http.StatusOK, `{"groups":[{"displayName":"Gemini Models","buckets":[{"displayName":"Weekly Limit Remaining","window":"weekly","remainingFraction":0.5}]}]}`
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
			return 0, ""
		}
	})

	if _, err := Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(triedPairs) < 2 {
		t.Fatalf("expected candidate search across extracted pairs, got %d attempt(s)", len(triedPairs))
	}

	// Second refresh must hit the cached client on the first request.
	triedPairs = nil
	writeTokenFile(t, `{"token":{"access_token":"old2","refresh_token":"ref","expiry":"2020-01-01T00:00:00Z"}}`)
	if _, err := Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(triedPairs) != 1 {
		t.Fatalf("expected 1 cached refresh call, got %d", len(triedPairs))
	}
}
