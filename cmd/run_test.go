package cmd

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
)

func TestWriteLiveBodyOrderAndSkeleton(t *testing.T) {
	used := 10.0
	slots := []slot{
		{name: "Claude", done: true, report: &usage.Report{
			Provider: "Claude",
			Windows:  []usage.Window{{Label: "5h", UsedPercent: &used}},
		}},
		{name: "Codex"}, // pending
		{name: "Kimi", done: true, skip: true},
		{name: "Grok", done: true, err: errString("boom")},
	}

	var b strings.Builder
	writeLiveBody(&b, slots, time.Unix(0, 0).UTC())
	out := b.String()

	// Kimi is skipped; order is Claude, Codex skeleton, Grok error.
	claude := strings.Index(out, "Claude\n")
	codex := strings.Index(out, "Codex\n")
	grok := strings.Index(out, "Grok\n")
	if claude < 0 || codex < 0 || grok < 0 {
		t.Fatalf("missing sections:\n%s", out)
	}
	if !(claude < codex && codex < grok) {
		t.Fatalf("wrong order (claude=%d codex=%d grok=%d):\n%s", claude, codex, grok, out)
	}
	if !strings.Contains(out, "Codex\n─────\n  …\n") {
		t.Fatalf("codex skeleton missing:\n%s", out)
	}
	if !strings.Contains(out, "  boom\n") {
		t.Fatalf("grok error missing:\n%s", out)
	}
	if strings.Contains(out, "Kimi") {
		t.Fatalf("skipped provider should be absent:\n%s", out)
	}
}

func TestWriteLiveBodyEmpty(t *testing.T) {
	slots := []slot{
		{name: "Claude", done: true, skip: true},
		{name: "Codex", done: true, skip: true},
	}
	var b strings.Builder
	writeLiveBody(&b, slots, time.Unix(0, 0).UTC())
	if !strings.Contains(b.String(), "No configured providers found") {
		t.Fatalf("expected empty-state message, got:\n%s", b.String())
	}
}

func TestWriteLiveBodyPendingOnlyNoEmptyMessage(t *testing.T) {
	slots := []slot{
		{name: "Claude"}, // pending
	}
	var b strings.Builder
	writeLiveBody(&b, slots, time.Unix(0, 0).UTC())
	out := b.String()
	if strings.Contains(out, "No configured providers found") {
		t.Fatalf("pending view should not show empty-state:\n%s", out)
	}
	if !strings.Contains(out, "  …\n") {
		t.Fatalf("expected skeleton:\n%s", out)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestWatchFlagRegistered(t *testing.T) {
	cmd := newRootCmd()
	if cmd.PersistentFlags().Lookup("watch") == nil {
		t.Fatal("expected --watch flag on root command")
	}
	if cmd.PersistentFlags().ShorthandLookup("w") == nil {
		t.Fatal("expected -w shorthand for --watch")
	}
	if watchInterval != 60*time.Second {
		t.Fatalf("watchInterval = %v, want 60s", watchInterval)
	}
}

func TestWatchLoopDisabledRunsOnce(t *testing.T) {
	var n atomic.Int32
	err := watchLoop(context.Background(), false, time.Hour, func(ctx context.Context) error {
		n.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Load() != 1 {
		t.Fatalf("calls = %d, want 1", n.Load())
	}
}

func TestWatchLoopRepeatsThenStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var n atomic.Int32
	err := watchLoop(ctx, true, 15*time.Millisecond, func(ctx context.Context) error {
		if n.Add(1) >= 3 {
			cancel()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Load() < 3 {
		t.Fatalf("calls = %d, want >= 3", n.Load())
	}
}

func TestWatchLoopCancelDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var n atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- watchLoop(ctx, true, time.Hour, func(ctx context.Context) error {
			n.Add(1)
			return nil
		})
	}()
	// First frame runs, then the loop blocks on the hour-long wait.
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchLoop did not return after cancel")
	}
	if n.Load() != 1 {
		t.Fatalf("calls = %d, want 1", n.Load())
	}
}

func TestWatchLoopCancelDuringFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := watchLoop(ctx, true, time.Hour, func(parent context.Context) error {
		cancel()
		return parent.Err()
	})
	if err != nil {
		t.Fatalf("cancel during fetch should return nil, got %v", err)
	}
}

func TestWatchLoopErrorExits(t *testing.T) {
	want := errors.New("fetch failed")
	var n atomic.Int32
	err := watchLoop(context.Background(), true, 20*time.Millisecond, func(ctx context.Context) error {
		n.Add(1)
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if n.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (should not retry on error)", n.Load())
	}
}
