package antigravity

import (
	"testing"
	"time"
)

func TestParseUsageWindowIDsAndLengths(t *testing.T) {
	report, err := parseUsage([]byte(`{"command":{"data":{"groups":[{"name":"Gemini 3 Pro","buckets":[
		{"name":"Weekly","window":"weekly","remaining_fraction":0.75,"reset_time":"2026-10-08T00:00:00Z"},
		{"name":"Session","window":"5h","remaining_fraction":1}]}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) != 2 {
		t.Fatalf("windows = %+v", report.Windows)
	}
	if w := report.Windows[0]; w.ID != "gemini-3-pro/weekly" || w.Label != "Gemini 3 Pro - Weekly" || w.Duration != 7*24*time.Hour {
		t.Fatalf("weekly window = %+v", w)
	}
	if w := report.Windows[1]; w.ID != "gemini-3-pro/session" || w.Duration != 5*time.Hour || !w.ResetsAt.IsZero() {
		t.Fatalf("session window = %+v", w)
	}
}
