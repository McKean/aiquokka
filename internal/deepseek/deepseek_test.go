package deepseek

import "testing"

func TestReportFromResponseSingleCurrency(t *testing.T) {
	resp := &balanceResponse{
		IsAvailable: true,
		BalanceInfos: []balanceInfo{{
			Currency:        "CNY",
			TotalBalance:    "110.00",
			GrantedBalance:  "10.00",
			ToppedUpBalance: "100.00",
		}},
	}

	report := reportFromResponse(resp)

	if report.Provider != "DeepSeek" {
		t.Fatalf("Provider = %q, want DeepSeek", report.Provider)
	}
	if len(report.Windows) != 1 {
		t.Fatalf("Windows = %d, want 1", len(report.Windows))
	}
	w := report.Windows[0]
	if w.Label != "Balance" {
		t.Fatalf("Label = %q, want Balance", w.Label)
	}
	if w.Remaining == nil || *w.Remaining != 110.0 {
		t.Fatalf("Remaining = %v, want 110.0", w.Remaining)
	}
	if w.Currency != "CNY" {
		t.Fatalf("Currency = %q, want CNY", w.Currency)
	}
	if len(report.Extra) != 2 {
		t.Fatalf("Extra = %d, want 2", len(report.Extra))
	}
	if report.Extra[0].Label != "Granted" || report.Extra[0].Value != "¥10.00" {
		t.Fatalf("Extra[0] = %+v, want Granted ¥10.00", report.Extra[0])
	}
	if report.Extra[1].Label != "Topped up" || report.Extra[1].Value != "¥100.00" {
		t.Fatalf("Extra[1] = %+v, want Topped up ¥100.00", report.Extra[1])
	}
}

func TestReportFromResponseUnavailable(t *testing.T) {
	report := reportFromResponse(&balanceResponse{IsAvailable: false})
	if len(report.Windows) != 0 {
		t.Fatalf("Windows = %d, want 0", len(report.Windows))
	}
	if len(report.Extra) != 1 || report.Extra[0].Label != "Status" {
		t.Fatalf("Extra = %+v, want Status fact", report.Extra)
	}
}

func TestReportFromResponseMultipleCurrencies(t *testing.T) {
	resp := &balanceResponse{
		IsAvailable: true,
		BalanceInfos: []balanceInfo{
			{Currency: "USD", TotalBalance: "5.00", GrantedBalance: "5.00"},
			{Currency: "CNY", TotalBalance: "20.00", ToppedUpBalance: "20.00"},
		},
	}

	report := reportFromResponse(resp)

	if len(report.Windows) != 2 {
		t.Fatalf("Windows = %d, want 2", len(report.Windows))
	}
	if report.Windows[0].Label != "Balance (USD)" || report.Windows[1].Label != "Balance (CNY)" {
		t.Fatalf("labels = %q, %q", report.Windows[0].Label, report.Windows[1].Label)
	}
}

func TestParseMoney(t *testing.T) {
	if v, err := parseMoney(" 12.34 "); err != nil || v != 12.34 {
		t.Fatalf("parseMoney = %v, %v", v, err)
	}
	if _, err := parseMoney(""); err == nil {
		t.Fatal("parseMoney(\"\") should error")
	}
}
