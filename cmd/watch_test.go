package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestFormatCountdown(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{60 * time.Second, "1:00"},
		{59*time.Second + 200*time.Millisecond, "1:00"},
		{59 * time.Second, "0:59"},
		{5 * time.Second, "0:05"},
		{0, "0:00"},
		{-time.Second, "0:00"},
		{90 * time.Second, "1:30"},
	}
	for _, tt := range tests {
		if got := formatCountdown(tt.in); got != tt.want {
			t.Errorf("formatCountdown(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteWatchStatus(t *testing.T) {
	var b strings.Builder
	writeWatchStatus(&b, 0, 42*time.Second, true)
	out := b.String()
	if !strings.Contains(out, "next refresh in 0:42") {
		t.Fatalf("missing countdown:\n%s", out)
	}
	if !strings.Contains(out, "press ") || !strings.Contains(out, "to refresh") {
		t.Fatalf("missing refresh hint:\n%s", out)
	}
	if !strings.Contains(out, "to close") {
		t.Fatalf("missing quit hint:\n%s", out)
	}
	if !strings.Contains(out, "●") {
		t.Fatalf("missing pulse icon:\n%s", out)
	}

	b.Reset()
	writeWatchStatus(&b, 3, 5*time.Second, false)
	out = b.String()
	if strings.Contains(out, "press") {
		t.Fatalf("key hint should be absent when disabled:\n%s", out)
	}
	if !strings.Contains(out, "next refresh in 0:05") {
		t.Fatalf("missing countdown:\n%s", out)
	}
}

func TestPulseIntensityNonEmpty(t *testing.T) {
	if len(pulseIntensity) == 0 {
		t.Fatal("pulseIntensity is empty")
	}
}
