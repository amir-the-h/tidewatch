package cmd

import "github.com/spf13/cobra"

var xCmd = &cobra.Command{
	Use:   "x",
	Short: "X (Twitter) rule builder: terms and Filtered Stream rules",
}

func init() {
	rootCmd.AddCommand(xCmd)
}
