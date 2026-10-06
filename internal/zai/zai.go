package zai

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/McKean/aiquokka/internal/httpx"
	"github.com/McKean/aiquokka/internal/usage"
)

// baseURL returns the Z.ai API base URL (override via ZAI_BASE_URL). The
// China platform (open.bigmodel.cn) exposes the same endpoints.
func baseURL() string {
	if v := os.Getenv("ZAI_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.z.ai/api"
}

// tokenAccountsResponse mirrors GET /biz/tokenAccounts/list/my — the
// prepaid usage bundles ("resource packages") attached to the account.
type tokenAccountsResponse struct {
	Rows []bundle `json:"rows"`
}

// bundle is one prepaid token bundle. Amounts are token counts.
type bundle struct {
	ResourcePackageName string `json:"resourcePackageName"`
	SuitableModel       string `json:"suitableModel"`
	SuitableScene       string `json:"suitableScene"`
	Status              string `json:"status"`
	ConsumeType         string `json:"consumeType"`
	TokensMagnitude     int64  `json:"tokensMagnitude"`  // original size
	AvailableBalance    int64  `json:"availableBalance"` // tokens left
	ExpirationTime      string `json:"expirationTime"`
}

// accountReportResponse mirrors GET /biz/account/query-customer-account-report
// — the cash (pay-as-you-go) balance. Amounts are plain decimals.
type accountReportResponse struct {
	Code    int         `json:"code"`
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Data    accountData `json:"data"`
}

type accountData struct {
	Balance          float64 `json:"balance"`
	RechargeAmount   float64 `json:"rechargeAmount"`
	GiveAmount       float64 `json:"giveAmount"`
	TotalSpendAmount float64 `json:"totalSpendAmount"`
	FrozenBalance    float64 `json:"frozenBalance"`
}

// Fetch reports the Z.ai usage bundles and cash balance for the configured
// API key. Bundles are the prepaid token packages (e.g. a "20 million GLM-5.3
// trial pack"); each is model-specific, so a bundle can sit untouched while
// pay-as-you-go cash is spent on a different model.
func Fetch(ctx context.Context) (*usage.Report, error) {
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
	jwt, err := mintJWT(key)
	if err != nil {
		return nil, err
	}

	bundles, err := getBundles(ctx, jwt)
	if err != nil {
		return nil, err
	}
	cash, cashErr := getCash(ctx, jwt) // non-fatal: bundles alone are useful
	if cashErr != nil {
		cash = nil
	}

	report := reportFromBundles(bundles)
	appendCash(report, cash)
	if cashErr != nil {
		report.Extra = append(report.Extra, usage.Fact{Label: "Cash", Value: "unavailable"})
	}
	return report, nil
}

// get performs an authenticated GET and decodes the JSON body into out.
func get(ctx context.Context, jwt, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL()+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", jwt)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aiquokka")

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("Z.ai credentials were rejected (%s) — check ZAI_API_KEY", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s%s: %s: %s", baseURL(), path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

func getBundles(ctx context.Context, jwt string) ([]bundle, error) {
	var out tokenAccountsResponse
	if err := get(ctx, jwt, "/biz/tokenAccounts/list/my", &out); err != nil {
		return nil, err
	}
	return out.Rows, nil
}

func getCash(ctx context.Context, jwt string) (*accountData, error) {
	var out accountReportResponse
	if err := get(ctx, jwt, "/biz/account/query-customer-account-report", &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, fmt.Errorf("Z.ai account report: %s", out.Msg)
	}
	return &out.Data, nil
}

// reportFromBundles converts effective prepaid bundles into usage windows.
// Each window shows the consumed fraction of the bundle (used/total tokens);
// expiry and applicability are surfaced as extra facts since bundles do not
// "reset" — they simply expire.
func reportFromBundles(bundles []bundle) *usage.Report {
	report := &usage.Report{Provider: "Z.ai"}
	for _, b := range bundles {
		if !strings.EqualFold(b.Status, "EFFECTIVE") {
			continue
		}
		if b.TokensMagnitude <= 0 {
			continue
		}
		used := b.TokensMagnitude - b.AvailableBalance
		report.Windows = append(report.Windows, usage.Window{
			ID:    bundleID(b),
			Label: bundleLabel(b, bundles),
			Used:  &used,
			Limit: &b.TokensMagnitude,
		})
		report.Extra = append(report.Extra,
			usage.Fact{Label: bundleLabel(b, bundles), Value: b.ResourcePackageName},
		)
		if b.SuitableModel != "" {
			report.Extra = append(report.Extra,
				usage.Fact{Label: "Applies to", Value: b.SuitableModel},
			)
		}
		if exp := shortDate(b.ExpirationTime); exp != "" {
			report.Extra = append(report.Extra, usage.Fact{Label: "Expires", Value: exp})
		}
	}
	return report
}

// bundleID names a bundle by the model it is for, whether or not the label
// shows it: the label carries the model only when several bundles are listed.
// Two effective bundles for one model share an ID.
func bundleID(b bundle) string {
	if b.SuitableModel == "" {
		return "bundle"
	}
	return "bundle/" + usage.Slug(b.SuitableModel)
}

// bundleLabel names a bundle window, adding the model when several bundles
// (or the bundle itself) make the target model relevant.
func bundleLabel(b bundle, bundles []bundle) string {
	if len(bundles) > 1 && b.SuitableModel != "" {
		return "Bundle (" + b.SuitableModel + ")"
	}
	return "Bundle"
}

// appendCash adds the pay-as-you-go balance as a remaining-balance window
// (full bar that runs down to zero) with top-up facts, mirroring DeepSeek.
func appendCash(report *usage.Report, cash *accountData) {
	if cash == nil {
		return
	}
	balance := cash.Balance
	report.Windows = append(report.Windows, usage.Window{
		ID:        "cash",
		Label:     "Cash",
		Remaining: &balance,
		Currency:  currency(),
	})
	if cash.RechargeAmount > 0 {
		report.Extra = append(report.Extra, usage.Fact{
			Label: "Topped up",
			Value: usage.FormatMoney(cash.RechargeAmount, currency()),
		})
	}
	if cash.GiveAmount > 0 {
		report.Extra = append(report.Extra, usage.Fact{
			Label: "Granted",
			Value: usage.FormatMoney(cash.GiveAmount, currency()),
		})
	}
	if cash.TotalSpendAmount > 0 {
		report.Extra = append(report.Extra, usage.Fact{
			Label: "Spent",
			Value: usage.FormatMoney(cash.TotalSpendAmount, currency()),
		})
	}
}

// currency guesses the cash-balance currency from the API host: the
// international platform bills in USD, the China platform in CNY.
func currency() string {
	if strings.Contains(baseURL(), "bigmodel.cn") {
		return "CNY"
	}
	return "USD"
}

// shortDate trims a wire timestamp like "2026-11-28T15:34:31" to its date.
func shortDate(s string) string {
	if len(s) < 10 {
		return s
	}
	return s[:10]
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func hmacSign(secret []byte, data string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(data))
	return b64url(mac.Sum(nil))
}
