package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

var chainCmd = &cobra.Command{
	Use:   "chain",
	Short: "Manage pipelines (legacy alias for 'pipeline')",
}

var chainRunNewOnly bool

var chainRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Dispatch historical tweets through a pipeline",
	Long:  "Queue all eligible tweets through the pipeline's start node, replicating what the stream listener does on arrival. Use --new-only to skip tweets that already have executions for any action node in the pipeline.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := jsonBody(map[string]any{"new_only": chainRunNewOnly})
		if err != nil {
			return fmt.Errorf("pipeline run: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/pipelines/"+url.PathEscape(args[0])+"/run", body)
		if err != nil {
			return fmt.Errorf("pipeline run: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("pipeline run: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Queued %s tweets for pipeline #%s\n",
			formatField(result["queued"]), args[0])
		return nil
	},
}

func init() {
	chainRunCmd.Flags().BoolVar(&chainRunNewOnly, "new-only", false, "Only dispatch tweets without existing executions in this pipeline")
	chainCmd.AddCommand(chainRunCmd)
	rootCmd.AddCommand(chainCmd)
}
