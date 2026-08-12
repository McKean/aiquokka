package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/McKean/aiquokka/internal/usage"
	"gopkg.in/yaml.v3"
)

// structured reports whether a machine-readable format was requested.
func structured() bool { return jsonOut || yamlOut }

// emit writes v to stdout as JSON or YAML per the output flags.
func emit(v any) error {
	if yamlOut {
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(v); err != nil {
			return err
		}
		return enc.Close()
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// fetcher fetches a usage report for one provider.
type fetcher func(ctx context.Context) (*usage.Report, error)

// provider pairs a display name with its fetcher.
type provider struct {
	name  string
	fetch fetcher
}

// run executes a single provider fetch and renders the result honoring --json.
func run(f fetcher) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	report, err := f(ctx)
	if err != nil {
		return err
	}

	if structured() {
		return emit(report)
	}
	usage.Render(os.Stdout, report, time.Now())
	return nil
}

// fetchResult is one provider's outcome in the aggregate view.
type fetchResult struct {
	name   string
	report *usage.Report
	err    error
}

// runAll fetches every provider concurrently. On a TTY it shows a fixed-order
// skeleton for configured providers and rewrites the view as each result
// arrives. Non-TTY and --json/--yaml wait for every fetch, then emit once.
func runAll(providers []provider) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if structured() {
		return runAllStructured(ctx, providers)
	}
	if stdoutIsTTY() {
		return runAllLive(ctx, providers)
	}
	return runAllBatch(ctx, providers)
}

func runAllStructured(ctx context.Context, providers []provider) error {
	results := fetchAll(ctx, providers)
	out := map[string]any{}
	for _, r := range results {
		switch {
		case r.err == nil:
			out[strings.ToLower(r.name)] = r.report
		case usage.IsNotConfigured(r.err):
			// Skip providers the user doesn't use.
		default:
			out[strings.ToLower(r.name)] = map[string]string{"error": r.err.Error()}
		}
	}
	return emit(out)
}

// runAllBatch waits for every provider, then prints in fixed provider order.
// Used when stdout is not a TTY (pipes, redirects).
func runAllBatch(ctx context.Context, providers []provider) error {
	results := fetchAll(ctx, providers)
	now := time.Now()

	var reports []*usage.Report
	for _, r := range results {
		if r.err == nil {
			reports = append(reports, r.report)
		}
	}
	labelWidth := usage.LabelWidth(reports...)

	shown := 0
	for _, r := range results {
		if r.err != nil && usage.IsNotConfigured(r.err) {
			continue
		}
		if shown > 0 {
			fmt.Fprintln(os.Stdout)
		}
		shown++
		if r.err != nil {
			writeProviderError(os.Stdout, r.name, r.err)
			continue
		}
		usage.RenderAligned(os.Stdout, r.report, now, labelWidth)
	}
	if shown == 0 {
		fmt.Fprintln(os.Stdout, "No configured providers found. Log in with claude, codex, kimi, or grok.")
	}
	return nil
}

// configProbeGrace is how long we wait for cheap NotConfigured probes before
// painting the first skeleton, so providers the user doesn't use rarely flash.
const configProbeGrace = 80 * time.Millisecond

// slot is one provider's live-view state.
type slot struct {
	name   string
	done   bool
	skip   bool // not configured — omit from the view
	report *usage.Report
	err    error
}

// runAllLive paints a fixed-order skeleton, then rewrites the view in place as
// each provider finishes. Unconfigured providers are dropped as soon as their
// fetch reports NotConfigured.
func runAllLive(ctx context.Context, providers []provider) error {
	slots := make([]slot, len(providers))
	for i, p := range providers {
		slots[i].name = p.name
	}

	type event struct {
		i      int
		report *usage.Report
		err    error
	}
	ch := make(chan event, len(providers))
	for i, p := range providers {
		go func(i int, p provider) {
			r, err := p.fetch(ctx)
			ch <- event{i: i, report: r, err: err}
		}(i, p)
	}

	now := time.Now()
	printedLines := 0
	remaining := len(providers)
	painted := false

	grace := time.NewTimer(configProbeGrace)
	defer grace.Stop()
	var graceC <-chan time.Time = grace.C

	paint := func() {
		printedLines = paintLive(os.Stdout, slots, now, printedLines)
		painted = true
	}

	for remaining > 0 {
		select {
		case ev := <-ch:
			remaining--
			s := &slots[ev.i]
			s.done = true
			if usage.IsNotConfigured(ev.err) {
				s.skip = true
			} else {
				s.report = ev.report
				s.err = ev.err
			}
			// Paint once the grace window has opened, or immediately when the
			// last fetch returns (so a single slow provider still shows).
			if painted || graceC == nil || remaining == 0 {
				paint()
			}
		case <-graceC:
			graceC = nil
			paint()
		}
	}
	// If every provider was unconfigured and grace never forced a paint with
	// content, ensure the empty-state message is shown.
	if !painted {
		paint()
	}
	return nil
}

// paintLive rewrites the aggregate view from the top of the previous frame.
// Returns the number of lines just printed (for the next cursor-up).
func paintLive(w io.Writer, slots []slot, now time.Time, prevLines int) int {
	var body strings.Builder
	writeLiveBody(&body, slots, now)
	content := body.String()

	if prevLines > 0 {
		// Move to the start of the previous frame and clear downward.
		fmt.Fprintf(w, "\033[%dA\033[J", prevLines)
	}
	fmt.Fprint(w, content)

	if content == "" {
		return 0
	}
	return strings.Count(content, "\n")
}

// writeLiveBody renders the current fixed-order view into b.
func writeLiveBody(b *strings.Builder, slots []slot, now time.Time) {
	var reports []*usage.Report
	for _, s := range slots {
		if s.done && !s.skip && s.err == nil && s.report != nil {
			reports = append(reports, s.report)
		}
	}
	labelWidth := usage.LabelWidth(reports...)

	shown := 0
	allDone := true
	for _, s := range slots {
		if s.skip {
			continue
		}
		if !s.done {
			allDone = false
		}
		if shown > 0 {
			b.WriteByte('\n')
		}
		shown++
		switch {
		case !s.done:
			writeSkeleton(b, s.name)
		case s.err != nil:
			writeProviderError(b, s.name, s.err)
		default:
			usage.RenderAligned(b, s.report, now, labelWidth)
		}
	}
	if shown == 0 && allDone {
		b.WriteString("No configured providers found. Log in with claude, codex, kimi, or grok.\n")
	}
}

func writeSkeleton(w io.Writer, name string) {
	fmt.Fprintln(w, name)
	fmt.Fprintln(w, strings.Repeat("─", len(name)))
	fmt.Fprintln(w, "  …")
}

func writeProviderError(w io.Writer, name string, err error) {
	fmt.Fprintln(w, name)
	fmt.Fprintln(w, strings.Repeat("─", len(name)))
	fmt.Fprintf(w, "  %s\n", err.Error())
}

// fetchAll runs every provider concurrently and returns results in the same
// order as providers.
func fetchAll(ctx context.Context, providers []provider) []fetchResult {
	results := make([]fetchResult, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func(i int, p provider) {
			defer wg.Done()
			r, err := p.fetch(ctx)
			results[i] = fetchResult{name: p.name, report: r, err: err}
		}(i, p)
	}
	wg.Wait()
	return results
}

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
