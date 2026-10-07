package zai

import (
	"context"
	"net/http"
	"sync"
	"time"

	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/McKean/aiquokka/internal/httpx/httpxtest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiKeyFromPiConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	os.WriteFile(path, []byte(`{"providers":{"zai":{"apiKey":"abc.def"}}}`), 0o600)

	if got := piKey(path); got != "abc.def" {
		t.Fatalf("piKey = %q, want abc.def", got)
	}
}

func TestPiKeyMissing(t *testing.T) {
	if got := piKey(filepath.Join(t.TempDir(), "models.json")); got != "" {
		t.Fatalf("piKey = %q, want empty", got)
	}
}

func TestMintJWTShape(t *testing.T) {
	token, err := mintJWT("myid.mysecret")
	if err != nil {
		t.Fatalf("mintJWT: %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}
	decode := func(s string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			t.Fatalf("decoding segment: %v", err)
		}
		return b
	}

	var header struct {
		Alg      string `json:"alg"`
		SignType string `json:"sign_type"`
	}
	if err := json.Unmarshal(decode(parts[0]), &header); err != nil {
		t.Fatalf("header: %v", err)
	}
	if header.Alg != "HS256" || header.SignType != "SIGN" {
		t.Fatalf("header = %+v, want HS256/SIGN", header)
	}

	var payload struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(decode(parts[1]), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.APIKey != "myid" {
		t.Fatalf("api_key = %q, want myid", payload.APIKey)
	}

	mac := hmac.New(sha256.New, []byte("mysecret"))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != want {
		t.Fatalf("signature mismatch: got %q, want %q", parts[2], want)
	}
}

func TestMintJWTRejectsMalformedKey(t *testing.T) {
	for _, key := range []string{"", "no-dot", ".secret", "id."} {
		if _, err := mintJWT(key); err == nil {
			t.Errorf("mintJWT(%q) succeeded, want error", key)
		}
	}
}

func TestReportFromBundles(t *testing.T) {
	bundles := []bundle{{
		ResourcePackageName: "20 million GLM-5.3 trial packs",
		SuitableModel:       "glm-5.3",
		Status:              "EFFECTIVE",
		TokensMagnitude:     20_000_000,
		AvailableBalance:    18_000_000,
		ExpirationTime:      "2026-11-28T15:34:31",
	}}

	report := reportFromBundles(bundles)

	if report.Provider != "Z.ai" {
		t.Fatalf("Provider = %q, want Z.ai", report.Provider)
	}
	if len(report.Windows) != 1 {
		t.Fatalf("Windows = %d, want 1", len(report.Windows))
	}
	w := report.Windows[0]
	if w.Label != "Bundle" {
		t.Fatalf("Label = %q, want Bundle", w.Label)
	}
	if w.Used == nil || *w.Used != 2_000_000 {
		t.Fatalf("Used = %v, want 2000000", w.Used)
	}
	if w.Limit == nil || *w.Limit != 20_000_000 {
		t.Fatalf("Limit = %v, want 20000000", w.Limit)
	}
	if len(report.Extra) != 3 {
		t.Fatalf("Extra = %d, want 3", len(report.Extra))
	}
	if report.Extra[1].Label != "Applies to" || report.Extra[1].Value != "glm-5.3" {
		t.Fatalf("Extra[1] = %+v, want Applies to glm-5.3", report.Extra[1])
	}
	if report.Extra[2].Label != "Expires" || report.Extra[2].Value != "2026-11-28" {
		t.Fatalf("Extra[2] = %+v, want Expires 2026-11-28", report.Extra[2])
	}
}

func TestReportFromBundlesSkipsIneffective(t *testing.T) {
	bundles := []bundle{
		{Status: "EXPIRED", TokensMagnitude: 100, AvailableBalance: 0},
		{Status: "effective", TokensMagnitude: 50, AvailableBalance: 25},
	}
	report := reportFromBundles(bundles)
	if len(report.Windows) != 1 {
		t.Fatalf("Windows = %d, want 1", len(report.Windows))
	}
	if report.Windows[0].Limit == nil || *report.Windows[0].Limit != 50 {
		t.Fatalf("Limit = %v, want 50", report.Windows[0].Limit)
	}
}

func TestReportFromBundlesMultipleGetsModelLabel(t *testing.T) {
	bundles := []bundle{
		{Status: "EFFECTIVE", SuitableModel: "glm-5.3", TokensMagnitude: 100, AvailableBalance: 90},
		{Status: "EFFECTIVE", SuitableModel: "glm-5.3-flash", TokensMagnitude: 200, AvailableBalance: 100},
	}
	report := reportFromBundles(bundles)
	if len(report.Windows) != 2 {
		t.Fatalf("Windows = %d, want 2", len(report.Windows))
	}
	if report.Windows[0].Label != "Bundle (glm-5.3)" {
		t.Fatalf("Label = %q, want Bundle (glm-5.3)", report.Windows[0].Label)
	}
	if report.Windows[1].Label != "Bundle (glm-5.3-flash)" {
		t.Fatalf("Label = %q, want Bundle (glm-5.3-flash)", report.Windows[1].Label)
	}
}

func TestAppendCash(t *testing.T) {
	report := reportFromBundles(nil)
	appendCash(report, &accountData{
		Balance:          2.95,
		RechargeAmount:   3.0,
		GiveAmount:       0.0,
		TotalSpendAmount: 0.05,
	})

	if len(report.Windows) != 1 {
		t.Fatalf("Windows = %d, want 1", len(report.Windows))
	}
	w := report.Windows[0]
	if w.Label != "Cash" {
		t.Fatalf("Label = %q, want Cash", w.Label)
	}
	if w.Remaining == nil || *w.Remaining != 2.95 {
		t.Fatalf("Remaining = %v, want 2.95", w.Remaining)
	}
	if w.Currency != "USD" {
		t.Fatalf("Currency = %q, want USD (api.z.ai default)", w.Currency)
	}
	// Granted is zero-amount, so only Topped up and Spent appear.
	if len(report.Extra) != 2 {
		t.Fatalf("Extra = %d, want 2", len(report.Extra))
	}
	if report.Extra[0].Value != "$3.00" || report.Extra[1].Value != "$0.05" {
		t.Fatalf("Extra = %+v, want Topped up $3.00 and Spent $0.05", report.Extra)
	}
}

func TestAppendCashNil(t *testing.T) {
	report := reportFromBundles(nil)
	appendCash(report, nil)
	if len(report.Windows) != 0 {
		t.Fatalf("Windows = %d, want 0", len(report.Windows))
	}
}

// The label names the model only when there are several bundles; the ID
// always does.
func TestBundleAndCashWindowIDs(t *testing.T) {
	one := []bundle{{Status: "EFFECTIVE", SuitableModel: "GLM-5.3", TokensMagnitude: 100, AvailableBalance: 40}}
	report := reportFromBundles(one)
	if w := report.Windows[0]; w.ID != "bundle/glm-5.3" || w.Label != "Bundle" {
		t.Fatalf("bundle window = %+v", w)
	}
	appendCash(report, &accountData{Balance: 3})
	if w := report.Windows[1]; w.ID != "cash" {
		t.Fatalf("cash window = %+v", w)
	}
	if id := bundleID(bundle{}); id != "bundle" {
		t.Fatalf("bundleID without a model = %q", id)
	}
}

// Bundles and cash are asked for at once, not one after the other: the stub
// answers a request only once both are in flight.
func TestFetchAsksBundlesAndCashAtOnce(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "myid.mysecret")
	t.Setenv("ZAI_BASE_URL", "")
	var mu sync.Mutex
	waiting := 0
	both := make(chan struct{})
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		mu.Lock()
		waiting++
		if waiting == 2 {
			close(both)
		}
		mu.Unlock()
		select {
		case <-both:
		case <-time.After(2 * time.Second):
			t.Errorf("%s was not in flight together with the other call", r.URL.Path)
		}
		if strings.HasSuffix(r.URL.Path, "/tokenAccounts/list/my") {
			return http.StatusOK, `{"rows":[{"status":"EFFECTIVE","tokensMagnitude":100,"availableBalance":40}]}`
		}
		return http.StatusOK, `{"success":true,"data":{"balance":3.5}}`
	})

	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 2 || report.Windows[0].ID != "bundle" || report.Windows[1].ID != "cash" {
		t.Fatalf("windows = %+v", report.Windows)
	}
}

// A failed cash call stays non-fatal; a failed bundles call is the provider's
// error.
func TestFetchErrorsOfTheTwoCalls(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "myid.mysecret")
	t.Setenv("ZAI_BASE_URL", "")
	failing := ""
	httpxtest.Stub(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, failing) {
			return http.StatusInternalServerError, `down`
		}
		if strings.HasSuffix(r.URL.Path, "/tokenAccounts/list/my") {
			return http.StatusOK, `{"rows":[{"status":"EFFECTIVE","tokensMagnitude":100,"availableBalance":40}]}`
		}
		return http.StatusOK, `{"success":true,"data":{"balance":3.5}}`
	})

	failing = "/query-customer-account-report"
	report, err := Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 1 || report.Extra[len(report.Extra)-1].Value != "unavailable" {
		t.Fatalf("report without cash = %+v", report)
	}

	failing = "/tokenAccounts/list/my"
	if _, err := Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "down") {
		t.Fatalf("err = %v, want the bundles call's error", err)
	}
}
