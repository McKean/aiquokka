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

func TestParseUsageLabelsWindowsByLength(t *testing.T) {
	// agy 1.3 names its buckets after what is left ("Weekly Limit Remaining"),
	// while the percent aiquokka reports is what is used.
	report, err := parseUsage([]byte(`{"command":{"data":{"groups":[{"name":"Gemini Models","buckets":[
		{"id":"gemini-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":0.3},
		{"id":"gemini-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":0.8},
		{"id":"gemini-daily","name":"Daily Limit Remaining","window":"daily","remaining_fraction":0.5}]}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		id, label string
		used      float64
	}{
		{"gemini-models/weekly-limit-remaining", "Gemini Models - Weekly", 70},
		{"gemini-models/five-hour-limit-remaining", "Gemini Models - 5h", 20},
		{"gemini-models/daily-limit-remaining", "Gemini Models - Daily Limit", 50},
	}
	if len(report.Windows) != len(want) {
		t.Fatalf("windows = %+v", report.Windows)
	}
	for i, w := range report.Windows {
		if w.ID != want[i].id || w.Label != want[i].label {
			t.Errorf("window %d = id %q label %q, want id %q label %q", i, w.ID, w.Label, want[i].id, want[i].label)
		}
		if w.UsedPercent == nil || *w.UsedPercent-want[i].used > 1e-9 || want[i].used-*w.UsedPercent > 1e-9 {
			t.Errorf("window %d used = %v, want %v", i, w.UsedPercent, want[i].used)
		}
	}
}
