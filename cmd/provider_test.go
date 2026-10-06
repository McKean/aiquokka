package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/McKean/aiquokka/internal/usage"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, 0},
		{"not logged in", usage.NotConfigured("no Claude credentials"), ExitNotLoggedIn},
		{"not logged in, wrapped", fmt.Errorf("claude: %w", usage.NotConfigured("no login")), ExitNotLoggedIn},
		{"expired and not refreshed", usage.NoRefresh("Claude", "claude"), ExitFailure},
		{"any other failure", errors.New("503 Service Unavailable"), ExitFailure},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Errorf("%s: ExitCode = %d, want %d", c.name, got, c.want)
		}
	}
	if ExitNotLoggedIn == ExitFailure || ExitNotLoggedIn == 0 || ExitNotLoggedIn == 2 {
		t.Fatalf("ExitNotLoggedIn = %d must differ from success, failure and a wrong command line", ExitNotLoggedIn)
	}
}

// --no-refresh must reach every provider of a fetch, through its context.
func TestNoRefreshFlagReachesProviders(t *testing.T) {
	t.Cleanup(func() { noRefresh = false })
	allowed := func() []bool {
		var seen []bool
		p := provider{name: "Stub", fetch: func(ctx context.Context) (*usage.Report, error) {
			seen = append(seen, usage.RefreshAllowed(ctx))
			return &usage.Report{Provider: "Stub"}, nil
		}}
		fetchAll(fetchContext(context.Background()), []provider{p, p})
		return seen
	}

	noRefresh = false
	if got := allowed(); len(got) != 2 || !got[0] || !got[1] {
		t.Fatalf("without the flag refresh must be allowed, got %v", got)
	}
	noRefresh = true
	if got := allowed(); len(got) != 2 || got[0] || got[1] {
		t.Fatalf("with --no-refresh no provider may refresh, got %v", got)
	}
}

func TestNoRefreshIsAFlagOfEveryCommand(t *testing.T) {
	root := newRootCmd()
	for _, name := range []string{"claude", "codex", "kimi", "grok", "tray"} {
		sub, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if sub.InheritedFlags().Lookup("no-refresh") == nil {
			t.Errorf("%s does not take --no-refresh", name)
		}
	}
}
