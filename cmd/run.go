package cmd

import (
	"context"
	"encoding/json"
	"fmt"
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

// runAll fetches every provider concurrently and renders them all. A provider
// that errors is reported inline and does not abort the others. It returns an
// error only if every provider failed.
func runAll(providers []provider) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type result struct {
		name   string
		report *usage.Report
		err    error
	}
	results := make([]result, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Add(1)
		go func(i int, p provider) {
			defer wg.Done()
			r, err := p.fetch(ctx)
			results[i] = result{name: p.name, report: r, err: err}
		}(i, p)
	}
	wg.Wait()

	if structured() {
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

	now := time.Now()
	shown := 0
	for _, r := range results {
		// Silently skip providers that aren't set up for this user.
		if r.err != nil && usage.IsNotConfigured(r.err) {
			continue
		}
		if shown > 0 {
			fmt.Fprintln(os.Stdout)
		}
		shown++
		if r.err != nil {
			fmt.Fprintf(os.Stdout, "%s\n%s\n  %s\n", r.name,
				strings.Repeat("─", len(r.name)), r.err.Error())
			continue
		}
		usage.Render(os.Stdout, r.report, now)
	}
	if shown == 0 {
		fmt.Fprintln(os.Stdout, "No configured providers found. Log in with claude, codex, kimi, or grok.")
	}
	return nil
}
