// Package antigravity runs the Antigravity CLI's non-interactive /usage
// command and converts its JSON output to aiquokka's common usage model.
package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

type printOutput struct {
	Status  string `json:"status"`
	Command struct {
		Data struct {
			Groups []struct {
				Name    string `json:"name"`
				Buckets []struct {
					Name              string  `json:"name"`
					Window            string  `json:"window"`
					RemainingFraction float64 `json:"remaining_fraction"`
					ResetTime         string  `json:"reset_time"`
				} `json:"buckets"`
			} `json:"groups"`
		} `json:"data"`
	} `json:"command"`
}

// Fetch asks the installed agy CLI for its /usage view in print mode. Since
// agy 1.2.x this answers without opening a session or spending quota; agy
// stays responsible for credentials and token refresh.
func Fetch(ctx context.Context) (*usage.Report, error) {
	path, err := exec.LookPath("agy")
	if err != nil {
		return nil, usage.NotConfigured("agy not found — install and log in to the Antigravity CLI first")
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, path, "-p", "/usage", "--output-format", "json")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("Antigravity usage request timed out")
		}
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(strings.ToLower(msg), "sign in") || strings.Contains(strings.ToLower(msg), "login") {
			return nil, usage.NotConfigured("agy is not logged in — run `agy` and sign in first")
		}
		if msg == "" {
			return nil, fmt.Errorf("running agy usage command: %w", err)
		}
		return nil, fmt.Errorf("running agy usage command: %w: %s", err, msg)
	}

	return parseUsage(stdout.Bytes())
}

func parseUsage(raw []byte) (*usage.Report, error) {
	var out printOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decoding agy usage output: %w", err)
	}

	report := &usage.Report{Provider: "Antigravity", Plan: "Usage"}
	for _, group := range out.Command.Data.Groups {
		for _, b := range group.Buckets {
			used := (1.0 - b.RemainingFraction) * 100.0
			w := usage.Window{
				Label:       fmt.Sprintf("%s - %s", group.Name, b.Name),
				UsedPercent: &used,
			}
			if b.ResetTime != "" {
				w.ResetsAt, _ = time.Parse(time.RFC3339, b.ResetTime)
			}
			switch b.Window {
			case "weekly":
				w.Duration = 7 * 24 * time.Hour
			case "5h":
				w.Duration = 5 * time.Hour
			}
			report.Windows = append(report.Windows, w)
		}
	}
	if len(report.Windows) == 0 {
		return nil, fmt.Errorf("no quota buckets in agy usage output (agy output may have changed)")
	}
	return report, nil
}
