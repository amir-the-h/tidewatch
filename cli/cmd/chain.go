package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var chainCmd = &cobra.Command{
	Use:   "chain",
	Short: "Manage chains",
}

var chainRunNewOnly bool

var chainRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Dispatch historical tweets through a chain",
	Long:  "Queue all eligible tweets through the chain's start node, replicating what the stream listener does on arrival. Use --new-only to skip tweets that already have executions for any action node in the chain.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := jsonBody(map[string]any{"new_only": chainRunNewOnly})
		if err != nil {
			return fmt.Errorf("chain run: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/chains/"+args[0]+"/run", body)
		if err != nil {
			return fmt.Errorf("chain run: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("chain run: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Queued %s tweets for chain #%s\n",
			formatField(result["queued"]), args[0])
		return nil
	},
}

func init() {
	chainRunCmd.Flags().BoolVar(&chainRunNewOnly, "new-only", false, "Only dispatch tweets without existing executions in this chain")
	chainCmd.AddCommand(chainRunCmd)
	rootCmd.AddCommand(chainCmd)
}
