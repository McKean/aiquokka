package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/McKean/aiquokka/internal/httpx"
	"github.com/McKean/aiquokka/internal/usage"
)

// baseURL returns the DeepSeek API base URL (override via DEEPSEEK_BASE_URL).
func baseURL() string {
	if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.deepseek.com"
}

// balanceResponse mirrors GET /user/balance.
type balanceResponse struct {
	IsAvailable  bool          `json:"is_available"`
	BalanceInfos []balanceInfo `json:"balance_infos"`
}

// balanceInfo is one currency's balance breakdown. Amounts are strings on the
// wire (e.g. "110.00").
type balanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

// Fetch reports the DeepSeek account balance for the configured API key.
func Fetch(ctx context.Context) (*usage.Report, error) {
	key, err := loadKey()
	if err != nil {
		return nil, err
	}

	resp, err := getBalance(ctx, key)
	if err != nil {
		return nil, err
	}
	return reportFromResponse(resp), nil
}

// getBalance performs the authenticated balance request.
func getBalance(ctx context.Context, key string) (*balanceResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL()+"/user/balance", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aiquokka")

	resp, err := httpx.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("DeepSeek API key was rejected (%s) — check DEEPSEEK_API_KEY", resp.Status)
		}
		return nil, fmt.Errorf("%s/user/balance: %s: %s", baseURL(), resp.Status, strings.TrimSpace(string(body)))
	}
	var out balanceResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding DeepSeek balance: %w", err)
	}
	return &out, nil
}

// reportFromResponse converts a balance response into the shared model. The
// total balance becomes a remaining-balance window (a full bar that ends at
// zero), and the granted/topped-up split is surfaced as extra facts.
func reportFromResponse(resp *balanceResponse) *usage.Report {
	report := &usage.Report{Provider: "DeepSeek"}
	if resp == nil || !resp.IsAvailable {
		report.Extra = append(report.Extra, usage.Fact{Label: "Status", Value: "unavailable"})
		return report
	}

	multi := len(resp.BalanceInfos) > 1
	for _, info := range resp.BalanceInfos {
		total, err := parseMoney(info.TotalBalance)
		if err != nil {
			continue
		}
		tag := currencyTag(info, multi)
		report.Windows = append(report.Windows, usage.Window{
			Label:     "Balance" + tag,
			Remaining: &total,
			Currency:  info.Currency,
		})

		if granted, err := parseMoney(info.GrantedBalance); err == nil {
			report.Extra = append(report.Extra, usage.Fact{
				Label: "Granted" + tag,
				Value: usage.FormatMoney(granted, info.Currency),
			})
		}
		if toppedUp, err := parseMoney(info.ToppedUpBalance); err == nil {
			report.Extra = append(report.Extra, usage.Fact{
				Label: "Topped up" + tag,
				Value: usage.FormatMoney(toppedUp, info.Currency),
			})
		}
	}
	if len(report.Windows) == 0 {
		report.Extra = append(report.Extra, usage.Fact{Label: "Balance", Value: "unknown"})
	}
	return report
}

// currencyTag adds a " (CNY)" suffix when multiple currencies are reported so
// window and fact labels stay unambiguous.
func currencyTag(info balanceInfo, multi bool) string {
	if !multi || info.Currency == "" {
		return ""
	}
	return " (" + info.Currency + ")"
}

// parseMoney parses a decimal amount from the API's string representation.
func parseMoney(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty balance amount")
	}
	return strconv.ParseFloat(s, 64)
}
