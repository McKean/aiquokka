package cmd

import (
	"github.com/McKean/aiquokka/internal/antigravity"
	"github.com/spf13/cobra"
)

func newAntigravityCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "antigravity",
		Aliases: []string{"agy"},
		Short:   "Check Google Antigravity usage limits",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(antigravity.Fetch)
		},
	}
}
