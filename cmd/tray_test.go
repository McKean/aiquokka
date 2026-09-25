package cmd

import (
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

	flags := []string{"interval", "notify", "threshold", "notify-reset", "provider"}
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
	app.checkNotifications("Grok", pctWindow("Weekly", 0, now.Add(7*24*time.Hour)), now)
	if len(*sent) != 1 || !contains((*sent)[0].body, "reset") {
		t.Errorf("expected one reset alert, got %v", *sent)
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

func TestWindowTitle(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	win := pctWindow("Weekly", 44, now.Add(2*time.Hour))
	if got := windowTitle(win, now); got != "Weekly   44%   ·   resets in 2h0m (Fri 14:00)" {
		t.Errorf("unexpected title: %q", got)
	}

	used, limit := int64(3), int64(50)
	credits := usage.Window{Label: "Credits", Used: &used, Limit: &limit}
	if got := windowTitle(credits, now); got != "Credits   6%  (3/50)" {
		t.Errorf("unexpected credits title: %q", got)
	}

	if got := windowTitle(usage.Window{Label: "Spend"}, now); got != "Spend   —" {
		t.Errorf("unexpected unknown title: %q", got)
	}
	if contains(windowTitle(win, now), "[") {
		t.Errorf("title must not contain ASCII bars")
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
