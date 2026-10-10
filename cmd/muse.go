package cmd

import (
	"github.com/McKean/aiquokka/internal/muse"
	"github.com/spf13/cobra"
)

func newMuseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "muse",
		Short: "Muse 5-hour and weekly subscription limits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(muse.Fetch)
		},
	}
}
