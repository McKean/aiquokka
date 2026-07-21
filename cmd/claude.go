package cmd

import (
	"github.com/McKean/aiquokka/internal/claude"
	"github.com/spf13/cobra"
)

func newClaudeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claude",
		Short: "Claude 5-hour and weekly usage limits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(claude.Fetch)
		},
	}
}
