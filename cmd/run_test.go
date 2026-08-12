package cmd

import (
	"strings"
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
