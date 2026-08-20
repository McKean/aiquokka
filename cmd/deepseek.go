package cmd

import (
	"github.com/McKean/aiquokka/internal/deepseek"
	"github.com/spf13/cobra"
)

func newDeepseekCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deepseek",
		Short: "DeepSeek account balance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(deepseek.Fetch)
		},
	}
}
