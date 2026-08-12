package cmd

import (
	"github.com/McKean/aiquokka/internal/kiro"
	"github.com/spf13/cobra"
)

func newKiroCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kiro",
		Short: "Check Kiro CLI credits and usage limits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(kiro.Fetch)
		},
	}
}
