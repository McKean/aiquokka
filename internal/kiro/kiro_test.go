package kiro

import (
	"math"
	"testing"
	"time"
)

func TestParseUsage(t *testing.T) {
	now := time.Date(2026, time.August, 12, 13, 25, 0, 0, time.FixedZone("CEST", 2*60*60))
	raw := "\x1b[38;5;244mModel: \x1b[0mauto | Plan: KIRO PRO (/usage for more detail)\n" +
		"\x1b[1mEstimated Usage\x1b[0m | resets on 09/01 | \x1b[38;5;141mKIRO PRO\x1b[0m\n" +
		"\x1b[1mCredits\x1b[0m (2.94 of 1000 covered in plan)\n" +
		"████ 0%\n\n" +
		"Overages: \x1b[1mDisabled\x1b[0m \x1b[38;5;244m(managed by your organization)\x1b[0m\n"

	report, err := parseUsage(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Provider != "Kiro" || report.Plan != "KIRO PRO" {
		t.Fatalf("unexpected report identity: %#v", report)
	}
	if len(report.Windows) != 1 {
		t.Fatalf("got %d windows, want 1", len(report.Windows))
	}
	window := report.Windows[0]
	if window.UsedPercent == nil || math.Abs(*window.UsedPercent-0.294) > 0.000001 {
		t.Fatalf("used percent = %v, want 0.294", window.UsedPercent)
	}
	wantReset := time.Date(2026, time.September, 1, 0, 0, 0, 0, now.Location())
	if !window.ResetsAt.Equal(wantReset) {
		t.Fatalf("reset = %s, want %s", window.ResetsAt, wantReset)
	}
	if len(report.Extra) != 2 || report.Extra[0].Value != "2.94 / 1000 covered in plan" ||
		report.Extra[1].Value != "Disabled (managed by your organization)" {
		t.Fatalf("unexpected facts: %#v", report.Extra)
	}
}

func TestParseUsageUsesNextYearForPastReset(t *testing.T) {
	now := time.Date(2026, time.December, 15, 12, 0, 0, 0, time.UTC)
	report, err := parseUsage("Estimated Usage | resets on 01/01 | KIRO FREE\nCredits (10 of 50 included)\n", now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	if !report.Windows[0].ResetsAt.Equal(want) {
		t.Fatalf("reset = %s, want %s", report.Windows[0].ResetsAt, want)
	}
}

func TestParseUsageRejectsChangedOutput(t *testing.T) {
	if _, err := parseUsage("Estimated Usage | KIRO PRO", time.Now()); err == nil {
		t.Fatal("expected missing credits error")
	}
}
