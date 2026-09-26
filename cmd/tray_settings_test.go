package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTraySettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aiquokka", "tray.json")
	saved := trayConfig{interval: 5 * time.Minute, notify: false, threshold: 90, notifyReset: false, pinned: "Claude"}
	if err := saveTraySettings(path, saved); err != nil {
		t.Fatal(err)
	}
	got, err := loadTraySettings(path, defaultTrayConfig, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if got != saved {
		t.Errorf("got %+v, want %+v", got, saved)
	}
}

func TestTraySettingsFlagsWin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tray.json")
	if err := saveTraySettings(path, trayConfig{interval: 5 * time.Minute, notify: true, threshold: 90, notifyReset: true, pinned: "Codex"}); err != nil {
		t.Fatal(err)
	}
	cfg := defaultTrayConfig
	cfg.threshold = 70
	cfg.pinned = "Claude"
	got, err := loadTraySettings(path, cfg, func(name string) bool { return name == "threshold" || name == "pin" })
	if err != nil {
		t.Fatal(err)
	}
	if got.threshold != 70 || got.interval != 5*time.Minute || got.pinned != "Claude" {
		t.Errorf("expected flag threshold and saved interval, got %+v", got)
	}
}

func TestTraySettingsMissingAndInvalid(t *testing.T) {
	dir := t.TempDir()
	got, err := loadTraySettings(filepath.Join(dir, "none.json"), defaultTrayConfig, func(string) bool { return false })
	if err != nil || got != defaultTrayConfig {
		t.Errorf("missing file should yield defaults, got %+v, %v", got, err)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"threshold": 400, "interval": "10s"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = loadTraySettings(bad, defaultTrayConfig, func(string) bool { return false })
	if err != nil || got.threshold != defaultTrayConfig.threshold || got.interval != defaultTrayConfig.interval {
		t.Errorf("out-of-range threshold and interval should be ignored, got %+v, %v", got, err)
	}
}
