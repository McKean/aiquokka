// Package cmd wires up the aiquokka cobra command tree.
package cmd

import (
	"time"

	"github.com/McKean/aiquokka/internal/antigravity"
	"github.com/McKean/aiquokka/internal/claude"
	"github.com/McKean/aiquokka/internal/codex"
	"github.com/McKean/aiquokka/internal/copilot"
	"github.com/McKean/aiquokka/internal/deepseek"
	"github.com/McKean/aiquokka/internal/grok"
	"github.com/McKean/aiquokka/internal/kimi"
	"github.com/McKean/aiquokka/internal/kiro"
	"github.com/McKean/aiquokka/internal/usage"
	"github.com/McKean/aiquokka/internal/zai"
	"github.com/spf13/cobra"
)

// allProviders is the set rendered when aiquokka is run with no subcommand.
var allProviders = []provider{
	{name: "Claude", fetch: claude.Fetch},
	{name: "Codex", fetch: codex.Fetch},
	{name: "Kimi", fetch: kimi.Fetch},
	{name: "Grok", fetch: grok.Fetch},
	{name: "Copilot", fetch: copilot.Fetch},
	{name: "DeepSeek", fetch: deepseek.Fetch},
	{name: "Kiro", fetch: kiro.Fetch},
	{name: "Antigravity", fetch: antigravity.Fetch},
	{name: "Z.ai", fetch: zai.Fetch},
}

// Global output-format flags: emit raw structured output instead of the
// rendered bars.
var (
	jsonOut bool
	yamlOut bool
	watch   bool
)

// noRefresh forbids renewing an expired token or writing a stored login.
var noRefresh bool

// watchInterval is how long --watch waits between refreshes.
const watchInterval = 60 * time.Second

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "aiquokka",
		Short: "Check AI coding-assistant usage limits",
		Long: `aiquokka reports the usage limits of your AI coding subscriptions.

  aiquokka          all providers at once
  aiquokka claude   5-hour and weekly limits
  aiquokka codex    weekly limit and reset info
  aiquokka kimi     5-hour and weekly limits
  aiquokka grok     weekly usage limit
  aiquokka copilot  copilot chat/completions limits
  aiquokka deepseek  account balance
  aiquokka kiro     Kiro CLI monthly credits and overage status
  aiquokka agy      daily antigravity limits
  aiquokka zai      Z.ai usage bundles and cash balance

  --watch           refresh every 60s; press r to refresh now, q/Ctrl+C to stop`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAll(allProviders)
		},
	}
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "emit raw JSON instead of rendered output")
	root.PersistentFlags().BoolVar(&yamlOut, "yaml", false, "emit raw YAML instead of rendered output")
	root.PersistentFlags().BoolVar(&yamlOut, "yml", false, "alias for --yaml")
	root.PersistentFlags().BoolVarP(&watch, "watch", "w", false, "refresh every 60s (r refresh, q close)")
	root.PersistentFlags().BoolVar(&noRefresh, "no-refresh", false, "never renew an expired token or write a stored login; report the provider as not known instead")
	root.MarkFlagsMutuallyExclusive("json", "yaml")
	root.MarkFlagsMutuallyExclusive("json", "yml")

	root.AddCommand(newClaudeCmd())
	root.AddCommand(newCodexCmd())
	root.AddCommand(newKimiCmd())
	root.AddCommand(newGrokCmd())
	root.AddCommand(newCopilotCmd())
	root.AddCommand(newDeepseekCmd())
	root.AddCommand(newKiroCmd())
	root.AddCommand(newAntigravityCmd())
	root.AddCommand(newZaiCmd())
	root.AddCommand(newTrayCmd())
	return root
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}

// Exit statuses. A script tells "this provider has no login here" (nothing to
// report, and retrying will not help) from a failure that may pass. 2 is left
// free: shells and many tools use it for a wrong command line.
const (
	ExitFailure     = 1
	ExitNotLoggedIn = 3
)

// ExitCode maps the error Execute returned to the process's exit status.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case usage.IsNotConfigured(err):
		return ExitNotLoggedIn
	}
	return ExitFailure
}
