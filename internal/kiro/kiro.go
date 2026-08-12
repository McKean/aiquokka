// Package kiro runs Kiro CLI's built-in usage command and converts its output
// to aiquokka's common usage model.
package kiro

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

var (
	ansiPattern     = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)
	planPattern     = regexp.MustCompile(`(?im)^Estimated Usage\s*\|[^\n]*\|\s*(.+?)\s*$`)
	fallbackPlan    = regexp.MustCompile(`(?im)\bPlan:\s*([^|\n]+)`)
	creditsPattern  = regexp.MustCompile(`(?im)^Credits\s*\(\s*([0-9]+(?:\.[0-9]+)?)\s+of\s+([0-9]+(?:\.[0-9]+)?)\s+([^)]*?)\s*\)`)
	resetPattern    = regexp.MustCompile(`(?i)\bresets on\s+(\d{1,2})/(\d{1,2})\b`)
	overagesPattern = regexp.MustCompile(`(?im)^Overages:\s*([^\n]+)`)
)

// Fetch asks the installed Kiro CLI for the same billing and credits view its
// interactive /usage command displays. Kiro CLI remains responsible for its
// credentials and token refresh; aiquokka only parses the command's output.
func Fetch(ctx context.Context) (*usage.Report, error) {
	path, err := exec.LookPath("kiro-cli")
	if err != nil {
		return nil, usage.NotConfigured("kiro-cli not found — install and log in to Kiro CLI first")
	}

	out, err := exec.CommandContext(ctx, path, "chat", "--no-interactive", "/usage").CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("Kiro usage request timed out")
		}
		text := strings.TrimSpace(stripANSI(string(out)))
		if isLoginMessage(text) {
			return nil, usage.NotConfigured("Kiro CLI is not logged in — run `kiro-cli login`")
		}
		if text == "" {
			return nil, fmt.Errorf("running Kiro usage command: %w", err)
		}
		return nil, fmt.Errorf("running Kiro usage command: %w: %s", err, lastLines(text, 3))
	}

	report, err := parseUsage(string(out), time.Now())
	if err != nil {
		text := stripANSI(string(out))
		if isLoginMessage(text) {
			return nil, usage.NotConfigured("Kiro CLI is not logged in — run `kiro-cli login`")
		}
		return nil, err
	}
	return report, nil
}

func parseUsage(raw string, now time.Time) (*usage.Report, error) {
	text := normalizeOutput(raw)
	report := &usage.Report{Provider: "Kiro"}

	if match := planPattern.FindStringSubmatch(text); len(match) == 2 {
		report.Plan = strings.TrimSpace(match[1])
	} else if match := fallbackPlan.FindStringSubmatch(text); len(match) == 2 {
		report.Plan = strings.TrimSpace(match[1])
	}

	match := creditsPattern.FindStringSubmatch(text)
	if len(match) != 4 {
		return nil, fmt.Errorf("Kiro usage output did not contain a credits allowance (Kiro CLI output may have changed)")
	}
	used, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return nil, fmt.Errorf("parsing Kiro credits used: %w", err)
	}
	limit, err := strconv.ParseFloat(match[2], 64)
	if err != nil || limit <= 0 {
		return nil, fmt.Errorf("parsing Kiro credit limit %q", match[2])
	}
	percent := math.Min(100, math.Max(0, used/limit*100))
	window := usage.Window{Label: "Credits", UsedPercent: &percent}
	if reset := resetPattern.FindStringSubmatch(text); len(reset) == 3 {
		month, _ := strconv.Atoi(reset[1])
		day, _ := strconv.Atoi(reset[2])
		window.ResetsAt = nextReset(month, day, now)
	}
	report.Windows = append(report.Windows, window)
	report.Extra = append(report.Extra, usage.Fact{
		Label: "Credits",
		Value: fmt.Sprintf("%s / %s %s", match[1], match[2], strings.TrimSpace(match[3])),
	})

	if overages := overagesPattern.FindStringSubmatch(text); len(overages) == 2 {
		report.Extra = append(report.Extra, usage.Fact{
			Label: "Overages",
			Value: strings.Join(strings.Fields(overages[1]), " "),
		})
	}
	return report, nil
}

func normalizeOutput(raw string) string {
	text := stripANSI(raw)
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, "\n")
}

func stripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

func nextReset(month, day int, now time.Time) time.Time {
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}
	}
	loc := now.Location()
	candidate := time.Date(now.Year(), time.Month(month), day, 0, 0, 0, 0, loc)
	// Invalid calendar dates normalize into another month; reject them.
	if int(candidate.Month()) != month || candidate.Day() != day {
		return time.Time{}
	}
	if !candidate.After(now) {
		candidate = time.Date(now.Year()+1, time.Month(month), day, 0, 0, 0, 0, loc)
	}
	return candidate
}

func isLoginMessage(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "not logged in") ||
		strings.Contains(lower, "please log in") ||
		strings.Contains(lower, "kiro-cli login")
}

func lastLines(text string, count int) string {
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' })
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, " | ")
}
