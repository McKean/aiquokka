package cmd

import (
	"github.com/McKean/aiquokka/internal/grok"
	"github.com/spf13/cobra"
)

func newGrokCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grok",
		Short: "Grok usage limits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(grok.Fetch)
		},
	}
}
