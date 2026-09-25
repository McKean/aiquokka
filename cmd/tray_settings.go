package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type traySettings struct {
	Interval    string `json:"interval,omitempty"`
	Notify      *bool  `json:"notify,omitempty"`
	Threshold   *int   `json:"threshold,omitempty"`
	NotifyReset *bool  `json:"notify_reset,omitempty"`
}

func defaultTraySettingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "aiquokka", "tray.json")
}

func loadTraySettings(path string, cfg trayConfig, changed func(string) bool) (trayConfig, error) {
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	var s traySettings
	if err := json.Unmarshal(data, &s); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.Interval != "" && !changed("interval") {
		if d, err := time.ParseDuration(s.Interval); err == nil && d >= trayMinInterval {
			cfg.interval = d
		}
	}
	if s.Notify != nil && !changed("notify") {
		cfg.notify = *s.Notify
	}
	if s.Threshold != nil && !changed("threshold") && validThreshold(*s.Threshold) {
		cfg.threshold = *s.Threshold
	}
	if s.NotifyReset != nil && !changed("notify-reset") {
		cfg.notifyReset = *s.NotifyReset
	}
	return cfg, nil
}

func saveTraySettings(path string, cfg trayConfig) error {
	if path == "" {
		return nil
	}
	s := traySettings{
		Interval:    cfg.interval.String(),
		Notify:      &cfg.notify,
		Threshold:   &cfg.threshold,
		NotifyReset: &cfg.notifyReset,
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func validThreshold(t int) bool {
	return t >= 1 && t <= 100
}
