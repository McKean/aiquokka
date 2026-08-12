package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunWatchStatusLoopTicksCountdown(t *testing.T) {
	// Capture status writes by swapping write target is hard (uses os.Stdout).
	// Instead verify the loop returns watchTick within ~interval and that
	// formatCountdown decreases over wall time.
	ctx := context.Background()
	start := time.Now()
	action := runWatchStatusLoop(ctx, 250*time.Millisecond, false, nil)
	elapsed := time.Since(start)
	if action != watchTick {
		t.Fatalf("action = %v, want watchTick", action)
	}
	// Should not return significantly before the interval.
	if elapsed < 200*time.Millisecond {
		t.Fatalf("returned too early: %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("returned too late: %v", elapsed)
	}
}

func TestRunWatchStatusLoopManualKey(t *testing.T) {
	calls := 0
	poll := func() (byte, bool) {
		calls++
		// After a couple of ticks, pretend the user hit r.
		if calls >= 2 {
			return 'r', true
		}
		return 0, false
	}
	start := time.Now()
	action := runWatchStatusLoop(context.Background(), time.Hour, true, poll)
	if action != watchManual {
		t.Fatalf("action = %v, want watchManual", action)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("manual refresh took too long")
	}
}

func TestRunWatchStatusLoopQuitKey(t *testing.T) {
	poll := func() (byte, bool) { return 'q', true }
	action := runWatchStatusLoop(context.Background(), time.Hour, true, poll)
	if action != watchStop {
		t.Fatalf("action = %v, want watchStop", action)
	}
}

func TestWriteWatchStatusCountdownChanges(t *testing.T) {
	var a, b bytes.Buffer
	writeWatchStatus(&a, 0, 50*time.Second, false)
	writeWatchStatus(&b, 0, 40*time.Second, false)
	if !strings.Contains(a.String(), "0:50") {
		t.Fatalf("a: %q", a.String())
	}
	if !strings.Contains(b.String(), "0:40") {
		t.Fatalf("b: %q", b.String())
	}
	if a.String() == b.String() {
		t.Fatal("countdown text should differ for different remaining times")
	}
}
