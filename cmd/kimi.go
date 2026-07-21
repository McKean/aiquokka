package cmd

import (
	"github.com/McKean/aiquokka/internal/kimi"
	"github.com/spf13/cobra"
)

func newKimiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kimi",
		Short: "Kimi 5-hour and weekly usage limits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(kimi.Fetch)
		},
	}
}
