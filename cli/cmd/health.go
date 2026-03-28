package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check the health of the iran-watcher API",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/health")
		if err != nil {
			return fmt.Errorf("health: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("health: decode response: %w", err)
		}

		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("health: marshal output: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(healthCmd)
}
