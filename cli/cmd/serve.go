package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the iran-watcher server",
	Long:  "Note: use 'iran-watcher serve' from cmd/iran-watcher binary.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "Use 'iran-watcher serve' from cmd/iran-watcher binary")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
