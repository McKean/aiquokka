package cmd

import (
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

func TestPrefsViewReflectsSettings(t *testing.T) {
	app := newTrayApp(nil, trayConfig{interval: 5 * time.Minute, notify: true, threshold: 90, notifyReset: false, pinned: "Codex"})
	app.lastResults = []fetchResult{
		{name: "Claude", report: &usage.Report{Provider: "Claude"}},
		{name: "Codex", report: &usage.Report{Provider: "Codex"}},
		{name: "Kiro", err: usage.NotConfigured("no")},
	}
	v := app.prefsView()
	if !v.notify || v.notifyReset {
		t.Errorf("checkboxes wrong: %+v", v)
	}
	if v.thresholds[v.threshold] != "90%" {
		t.Errorf("selected threshold = %q", v.thresholds[v.threshold])
	}
	if v.intervals[v.interval] != "5 minutes" {
		t.Errorf("selected interval = %q", v.intervals[v.interval])
	}
	want := []string{"Highest of all providers", "Claude", "Codex"}
	if len(v.pins) != len(want) {
		t.Fatalf("pins = %v, want %v", v.pins, want)
	}
	for i := range want {
		if v.pins[i] != want[i] {
			t.Fatalf("pins = %v, want %v", v.pins, want)
		}
	}
	if v.pinValues[v.pin] != "Codex" {
		t.Errorf("selected pin = %q", v.pinValues[v.pin])
	}
}

func TestApplyPref(t *testing.T) {
	app := newTrayApp(nil, trayConfig{interval: time.Minute, notify: true, threshold: 80, notifyReset: true})
	app.lastResults = []fetchResult{{name: "Claude", report: &usage.Report{Provider: "Claude"}}}
	app.shownPrefs = app.prefsView()
	app.alertedWindows["Claude:5h"] = levelCritical

	app.applyPref(prefNotify, 0)
	app.applyPref(prefNotifyReset, 0)
	app.applyPref(prefThreshold, 0)
	app.applyPref(prefPin, 1)
	app.applyPref(prefInterval, 3)

	if app.notify || app.notifyReset {
		t.Errorf("checkboxes not applied: notify=%v reset=%v", app.notify, app.notifyReset)
	}
	if app.threshold != trayThresholdChoices[0] || len(app.alertedWindows) != 0 {
		t.Errorf("threshold not applied or alerts not re-armed: %d %v", app.threshold, app.alertedWindows)
	}
	if app.pinned != "Claude" {
		t.Errorf("pin not applied: %q", app.pinned)
	}
	if app.interval != 15*time.Minute {
		t.Errorf("interval not applied: %v", app.interval)
	}

	app.applyPref(prefPin, 0)
	if app.pinned != "" {
		t.Errorf("first pin choice should mean highest of all, got %q", app.pinned)
	}
	app.applyPref(prefPin, 99)
	app.applyPref(prefInterval, -1)
	if app.pinned != "" || app.interval != 15*time.Minute {
		t.Errorf("out-of-range values must be ignored")
	}
}

func TestIntervalLabel(t *testing.T) {
	for d, want := range map[time.Duration]string{time.Minute: "1 minute", 2 * time.Minute: "2 minutes", 15 * time.Minute: "15 minutes", 90 * time.Second: "1m30s"} {
		if got := intervalLabel(d); got != want {
			t.Errorf("intervalLabel(%v) = %q, want %q", d, got, want)
		}
	}
}
