package cmd

import (
	"github.com/McKean/aiquokka/internal/copilot"
	"github.com/spf13/cobra"
)

func newCopilotCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "copilot",
		Short: "Check GitHub Copilot usage limits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(copilot.Fetch)
		},
	}
}
