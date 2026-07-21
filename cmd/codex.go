package cmd

import (
	"github.com/McKean/aiquokka/internal/codex"
	"github.com/spf13/cobra"
)

func newCodexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "codex",
		Short: "Codex weekly usage limit and reset info",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(codex.Fetch)
		},
	}
}
