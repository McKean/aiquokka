// Package cmd wires up the aiquokka cobra command tree.
package cmd

import (
	"github.com/McKean/aiquokka/internal/antigravity"
	"github.com/McKean/aiquokka/internal/claude"
	"github.com/McKean/aiquokka/internal/codex"
	"github.com/McKean/aiquokka/internal/copilot"
	"github.com/McKean/aiquokka/internal/grok"
	"github.com/McKean/aiquokka/internal/kimi"
	"github.com/McKean/aiquokka/internal/kiro"
	"github.com/spf13/cobra"
)

// allProviders is the set rendered when aiquokka is run with no subcommand.
var allProviders = []provider{
	{name: "Claude", fetch: claude.Fetch},
	{name: "Codex", fetch: codex.Fetch},
	{name: "Kimi", fetch: kimi.Fetch},
	{name: "Grok", fetch: grok.Fetch},
	{name: "Copilot", fetch: copilot.Fetch},
	{name: "Kiro", fetch: kiro.Fetch},
	{name: "Antigravity", fetch: antigravity.Fetch},
}

// Global output-format flags: emit raw structured output instead of the
// rendered bars.
var (
	jsonOut bool
	yamlOut bool
)

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
  aiquokka kiro     Kiro CLI monthly credits and overage status
  aiquokka agy      daily antigravity limits`,
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
	root.MarkFlagsMutuallyExclusive("json", "yaml")
	root.MarkFlagsMutuallyExclusive("json", "yml")

	root.AddCommand(newClaudeCmd())
	root.AddCommand(newCodexCmd())
	root.AddCommand(newKimiCmd())
	root.AddCommand(newGrokCmd())
	root.AddCommand(newCopilotCmd())
	root.AddCommand(newKiroCmd())
	root.AddCommand(newAntigravityCmd())
	return root
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}
