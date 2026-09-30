package cmd

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

func TestNewTrayCmd(t *testing.T) {
	cmd := newTrayCmd()
	if cmd.Use != "tray" {
		t.Errorf("expected command Use to be 'tray', got %q", cmd.Use)
	}

	expectedAliases := []string{"bar", "menu", "systray"}
	for _, alias := range expectedAliases {
		found := false
		for _, a := range cmd.Aliases {
			if a == alias {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected alias %q not found in %v", alias, cmd.Aliases)
		}
	}

	flags := []string{"interval", "notify", "threshold", "notify-reset", "provider", "pin"}
	for _, flag := range flags {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected flag %q not found on tray command", flag)
		}
	}
}

func TestTrayCmdRejectsFastPolling(t *testing.T) {
	cmd := newTrayCmd()
	cmd.SetArgs([]string{"--interval", "30s"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !contains(err.Error(), "at least 1m") {
		t.Errorf("expected --interval 30s to be rejected, got %v", err)
	}
}

func TestTrayAppInitialization(t *testing.T) {
	cfg := trayConfig{
		interval:    30 * time.Second,
		notify:      true,
		threshold:   85,
		notifyReset: true,
	}

	providers := []provider{
		{name: "TestProvider", fetch: nil},
	}

	app := newTrayApp(providers, cfg)
	if app.interval != 30*time.Second {
		t.Errorf("expected interval 30s, got %v", app.interval)
	}
	if app.threshold != 85 {
		t.Errorf("expected threshold 85, got %d", app.threshold)
	}
	if !app.notify {
		t.Errorf("expected notify to be true")
	}
	if len(app.templateIcon) == 0 {
		t.Errorf("expected template icon to be generated")
	}
	if len(app.regularIcon) == 0 {
		t.Errorf("expected regular icon to be generated")
	}
}

type sentAlert struct {
	title, body string
	urgent      bool
}

func newRecordingApp(threshold int) (*trayApp, *[]sentAlert) {
	app := newTrayApp(nil, trayConfig{interval: time.Minute, notify: true, threshold: threshold, notifyReset: true})
	var sent []sentAlert
	app.send = func(title, body string, urgent bool) {
		sent = append(sent, sentAlert{title, body, urgent})
	}
	return app, &sent
}

func pctWindow(label string, pct float64, resetsAt time.Time) usage.Window {
	return usage.Window{Label: label, UsedPercent: &pct, ResetsAt: resetsAt}
}

func TestTrayAppNotificationsLogic(t *testing.T) {
	app, sent := newRecordingApp(80)
	now := time.Now()
	resets := now.Add(2 * time.Hour)

	app.checkNotifications("Claude", pctWindow("5h", 70, resets), now)
	if len(*sent) != 0 {
		t.Fatalf("expected no alert at 70%%, got %v", *sent)
	}

	app.checkNotifications("Claude", pctWindow("5h", 85, resets), now)
	app.checkNotifications("Claude", pctWindow("5h", 88, resets), now)
	if len(*sent) != 1 || (*sent)[0].urgent {
		t.Fatalf("expected exactly one non-urgent alert above threshold, got %v", *sent)
	}

	app.checkNotifications("Claude", pctWindow("5h", 100, resets), now)
	if len(*sent) != 2 || !(*sent)[1].urgent {
		t.Fatalf("expected an urgent exhausted alert at 100%%, got %v", *sent)
	}

	app.checkNotifications("Claude", pctWindow("5h", 50, resets), now)
	if _, armed := app.alertedWindows["Claude:5h"]; armed {
		t.Errorf("expected alert state to clear after dropping to 50%%")
	}
	app.checkNotifications("Claude", pctWindow("5h", 81, resets), now)
	if len(*sent) != 3 {
		t.Errorf("expected alert to fire again after re-arming, got %v", *sent)
	}
}

func TestTrayAppNotifyDisabledStillTracksState(t *testing.T) {
	app, sent := newRecordingApp(80)
	app.notify = false
	now := time.Now()
	app.checkNotifications("Claude", pctWindow("5h", 90, now.Add(time.Hour)), now)
	if len(*sent) != 0 {
		t.Errorf("expected no alert with notify disabled, got %v", *sent)
	}
	if app.alertedWindows["Claude:5h"] != levelCritical {
		t.Errorf("expected window marked as alerted")
	}
}

func TestTrayAppAlertsOnUsedLimitWindows(t *testing.T) {
	app, sent := newRecordingApp(80)
	used, limit := int64(45), int64(50)
	app.checkNotifications("Kiro", usage.Window{Label: "Credits", Used: &used, Limit: &limit}, time.Now())
	if len(*sent) != 1 || !contains((*sent)[0].body, "90%") {
		t.Errorf("expected alert for 45/50 credits, got %v", *sent)
	}
}

func TestSetThresholdRearmsAlerts(t *testing.T) {
	app, sent := newRecordingApp(80)
	now := time.Now()
	app.checkNotifications("Codex", pctWindow("Weekly", 60, now.Add(time.Hour)), now)
	app.setThreshold(50)
	app.checkNotifications("Codex", pctWindow("Weekly", 60, now.Add(time.Hour)), now)
	if len(*sent) != 1 {
		t.Errorf("expected alert after lowering threshold to 50%%, got %v", *sent)
	}
}

func TestResetNotification(t *testing.T) {
	app, sent := newRecordingApp(80)
	now := time.Now()
	app.checkNotifications("Grok", pctWindow("Weekly", 10, now.Add(time.Hour)), now)
	later := now.Add(time.Hour + time.Minute)
	app.checkNotifications("Grok", pctWindow("Weekly", 0, later.Add(7*24*time.Hour)), later)
	if len(*sent) != 1 || !contains((*sent)[0].body, "reset") {
		t.Errorf("expected one reset alert, got %v", *sent)
	}
}

func TestNoResetNotificationWhenResetTimeDrifts(t *testing.T) {
	app, sent := newRecordingApp(80)
	now := time.Now()
	// Codex computes the reset as now plus a countdown, so it moves a
	// second or two between fetches.
	app.checkNotifications("Codex", pctWindow("Weekly", 10, now.Add(3*24*time.Hour)), now)
	next := now.Add(time.Minute)
	app.checkNotifications("Codex", pctWindow("Weekly", 10, next.Add(3*24*time.Hour-58*time.Second)), next)
	// A rolling window that pushes its reset later before reaching it.
	app.checkNotifications("Claude", pctWindow("5h", 0, now.Add(5*time.Hour)), now)
	app.checkNotifications("Claude", pctWindow("5h", 0, next.Add(5*time.Hour)), next)
	if len(*sent) != 0 {
		t.Errorf("expected no reset alerts, got %v", *sent)
	}
}

func TestLevelFor(t *testing.T) {
	cases := []struct {
		pct       float64
		threshold int
		want      limitLevel
	}{
		{10, 80, levelOK},
		{59, 80, levelOK},
		{60, 80, levelWarn},
		{80, 80, levelCritical},
		{100, 80, levelExhausted},
		{40, 50, levelWarn},
		{55, 50, levelCritical},
	}
	for _, c := range cases {
		if got := levelFor(c.pct, c.threshold); got != c.want {
			t.Errorf("levelFor(%v, %d) = %v, want %v", c.pct, c.threshold, got, c.want)
		}
	}
}

func TestSummarizeAndSummaryLine(t *testing.T) {
	now := time.Now()
	results := []fetchResult{
		{name: "Claude", report: &usage.Report{Provider: "Claude", Windows: []usage.Window{
			pctWindow("5h", 85, now), pctWindow("Weekly", 30, now),
		}}},
		{name: "Codex", report: &usage.Report{Provider: "Codex", Windows: []usage.Window{
			pctWindow("Weekly", 92, now),
		}}},
	}
	s := summarize(results, 80)
	if s.active != 2 || s.overThreshold != 2 || s.highestPct != 92 || s.highestProvider != "Codex" {
		t.Fatalf("unexpected summary: %+v", s)
	}
	if got, alert := summaryLine(s, 80); got != "2 limits at or above 80%" || !alert {
		t.Errorf("unexpected summary line: %q %v", got, alert)
	}
	if got, alert := summaryLine(summarize(results, 95), 95); got != "All limits below 95%" || alert {
		t.Errorf("unexpected summary line: %q %v", got, alert)
	}
}

func TestTextColumnsRow(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	used, limit := int64(3), int64(50)
	credits := usage.Window{Label: "Credits", Used: &used, Limit: &limit}
	weekly := pctWindow("Weekly Fable", 44, now.Add(2*time.Hour))
	fiveHour := pctWindow("5h", 3, now.Add(50*time.Hour))
	cols := newTextColumns([]usage.Window{credits, weekly, fiveHour})

	cases := map[string]struct {
		w    usage.Window
		want string
	}{
		"same day": {weekly, "    Weekly Fable         44%   resets 14:00"},
		"weekday":  {fiveHour, "    5h                    3%   resets Sun 14:00"},
		"counts":   {credits, "    Credits        6% (3/50)"},
		"unknown":  {usage.Window{Label: "Spend"}, "    Spend                  —"},
	}
	for name, tc := range cases {
		if got := cols.row(tc.w, now); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestDisplayPlan(t *testing.T) {
	cases := []struct{ provider, plan, want string }{
		{"Claude", "max/default_claude_max_20x", "Max 20x"},
		{"Claude", "pro", "Pro"},
		{"Claude", "", ""},
		{"Codex", "plus", "Plus"},
		{"Grok", "XPremium", "XPremium"},
		{"Kiro", "KIRO PRO", "KIRO PRO"},
	}
	for _, tc := range cases {
		if got := displayPlan(tc.provider, tc.plan); got != tc.want {
			t.Errorf("displayPlan(%q, %q) = %q, want %q", tc.provider, tc.plan, got, tc.want)
		}
	}
}

func TestMenuLabelEscapesMnemonics(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("mnemonic escaping only applies to DBusMenu")
	}
	if got := menuLabel("default_claude_max_20x"); got != "default__claude__max__20x" {
		t.Errorf("unexpected label: %q", got)
	}
}

func TestWindowTooltipPace(t *testing.T) {
	now := time.Now()
	win := pctWindow("5h", 70, now.Add(2*time.Hour+30*time.Minute))
	win.Duration = 5 * time.Hour
	if got := windowTooltip(win, now); got != "50% of the window elapsed · 20 pts over pace" {
		t.Errorf("unexpected tooltip: %q", got)
	}
	under := 20.0
	win.UsedPercent = &under
	if got := windowTooltip(win, now); got != "50% of the window elapsed · 30 pts under pace" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

func TestShortDurationAndPct(t *testing.T) {
	for d, want := range map[time.Duration]string{30 * time.Second: "30s", time.Minute: "1m", 15 * time.Minute: "15m", 2 * time.Hour: "2h"} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
	if formatPct(0.08) != "0.1%" || formatPct(13) != "13%" || formatPct(0) != "0%" {
		t.Errorf("unexpected pct formatting")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && findSubstr(s, substr)))
}

func findSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPinnedStatus(t *testing.T) {
	now := time.Now()
	results := []fetchResult{
		{name: "Claude", report: &usage.Report{Provider: "Claude", Windows: []usage.Window{
			pctWindow("Weekly", 14, now), pctWindow("Weekly Fable", 19, now),
		}}},
		{name: "Codex", report: &usage.Report{Provider: "Codex", Windows: []usage.Window{pctWindow("Weekly", 49, now)}}},
		{name: "Kiro", err: usage.NotConfigured("kiro-cli not found")},
		{name: "Grok", err: errors.New("boom")},
	}

	v := pinnedStatus(results, "Claude")
	if !v.configured || v.pct != 19 || v.window != "Weekly Fable" {
		t.Errorf("pinned Claude should show its own highest window, got %+v", v)
	}
	if v := pinnedStatus(results, "Kiro"); v.configured {
		t.Errorf("not configured provider should not count as configured: %+v", v)
	}
	if v := pinnedStatus(results, "Grok"); !v.configured || v.err == nil {
		t.Errorf("provider with an error should keep the error: %+v", v)
	}
	if v := pinnedStatus(results, "Copilot"); v.configured || v.pct != -1 {
		t.Errorf("missing provider: %+v", v)
	}
}

func TestPinChoicesAndPinFor(t *testing.T) {
	results := []fetchResult{
		{name: "Claude", report: &usage.Report{Provider: "Claude"}},
		{name: "Kiro", err: usage.NotConfigured("no")},
	}
	got := pinChoices(results, "Codex")
	if len(got) != 2 || got[0] != "Claude" || got[1] != "Codex" {
		t.Errorf("pin choices should list configured providers plus the current pin, got %v", got)
	}

	all := []provider{{name: "Claude"}, {name: "Codex"}}
	if pinFor("CLAUDE", all) != "Claude" {
		t.Errorf("pin should match case-insensitively")
	}
	if pinFor("Claude", []provider{{name: "Codex"}}) != "" {
		t.Errorf("pin outside the watched providers should be dropped")
	}
	if pinFor("nope", all) != "" {
		t.Errorf("unknown pin should be dropped")
	}
}

func TestTrayCmdRejectsUnknownPin(t *testing.T) {
	cmd := newTrayCmd()
	cmd.SetArgs([]string{"--pin", "nope"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !contains(err.Error(), "unknown provider") {
		t.Errorf("expected --pin nope to be rejected, got %v", err)
	}
}
