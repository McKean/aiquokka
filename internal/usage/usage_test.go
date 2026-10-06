package usage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestWindowJSONCarriesIDAndLength(t *testing.T) {
	used := 24.0
	w := Window{
		ID:          "session",
		Label:       "5h",
		UsedPercent: &used,
		ResetsAt:    time.Date(2026, 10, 7, 19, 0, 0, 0, time.UTC),
		Duration:    5 * time.Hour,
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"session","label":"5h","used_percent":24,"resets_at":"2026-10-07T19:00:00Z","duration_seconds":18000}`
	if string(data) != want {
		t.Fatalf("JSON = %s\nwant   %s", data, want)
	}
}

// A window with no known reset or length used to print resets_at as year 1:
// omitempty never drops a time.Time.
func TestWindowJSONLeavesOutUnknownResetAndLength(t *testing.T) {
	remaining := 110.0
	data, err := json.Marshal(Report{Provider: "DeepSeek", Windows: []Window{
		{ID: "balance/cny", Label: "Balance", Remaining: &remaining, Currency: "CNY"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"provider":"DeepSeek","windows":[{"id":"balance/cny","label":"Balance","remaining":110,"currency":"CNY"}]}`
	if string(data) != want {
		t.Fatalf("JSON = %s\nwant   %s", data, want)
	}
}

func TestWindowYAMLMatchesJSONKeys(t *testing.T) {
	used := 24.0
	data, err := yaml.Marshal(Report{Provider: "Claude", Windows: []Window{
		{ID: "session", Label: "5h", UsedPercent: &used, Duration: 5 * time.Hour},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "provider: Claude\nwindows:\n    - id: session\n      label: 5h\n      used_percent: 24\n      duration_seconds: 18000\n"
	if string(data) != want {
		t.Fatalf("YAML =\n%s\nwant\n%s", data, want)
	}
}

func TestReportJSONAccount(t *testing.T) {
	data, err := json.Marshal(Report{Provider: "Grok", Account: NewAccount("u-1", "a@example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"account":{"id":"u-1","email":"a@example.com"}`) {
		t.Fatalf("account missing: %s", data)
	}
	if NewAccount("", "") != nil {
		t.Fatal("NewAccount with nothing known must be nil, so the key is left out")
	}
}

func TestLengthID(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                       "",
		5 * time.Hour:           "5h",
		7 * 24 * time.Hour:      "7d",
		90 * time.Minute:        "90m",
		90 * time.Second:        "90s",
		24 * time.Hour:          "1d",
		-time.Hour:              "",
		31 * 24 * time.Hour:     "31d",
		1500 * time.Millisecond: "1s",
	} {
		if got := LengthID(d); got != want {
			t.Errorf("LengthID(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Gemini 3 Pro":     "gemini-3-pro",
		"glm-5.3":          "glm-5.3",
		"  Claude / Opus ": "claude-opus",
		"":                 "",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWithoutRefresh(t *testing.T) {
	ctx := context.Background()
	if !RefreshAllowed(ctx) {
		t.Fatal("a plain context must allow refresh")
	}
	if RefreshAllowed(WithoutRefresh(ctx)) {
		t.Fatal("WithoutRefresh must forbid refresh")
	}
	err := NoRefresh("Claude", "claude")
	if !errors.Is(err, ErrNoRefresh) {
		t.Fatalf("NoRefresh does not match ErrNoRefresh: %v", err)
	}
	if IsNotConfigured(err) {
		t.Fatal("an expired token is not a missing login")
	}
	if !strings.Contains(err.Error(), "--no-refresh") || !strings.Contains(err.Error(), "`claude`") {
		t.Fatalf("message does not give the reason: %v", err)
	}
}
