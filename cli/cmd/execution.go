package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var executionCmd = &cobra.Command{
	Use:   "execution",
	Short: "Inspect action execution history",
}

var (
	execActionID string
	execTweetID  string
	execStatus   string
	execPage     int
	execPerPage  int
)

var executionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List action executions",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := url.Values{}
		if execActionID != "" {
			params.Set("action_id", execActionID)
		}
		if execTweetID != "" {
			params.Set("tweet_id", execTweetID)
		}
		if execStatus != "" {
			params.Set("status", execStatus)
		}
		if execPage > 0 {
			params.Set("page", fmt.Sprintf("%d", execPage))
		}
		if execPerPage > 0 {
			params.Set("per_page", fmt.Sprintf("%d", execPerPage))
		}
		reqURL := baseURL + "/api/executions"
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}
		resp, err := apiGet(reqURL)
		if err != nil {
			return fmt.Errorf("execution list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var execs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&execs); err != nil {
			return fmt.Errorf("execution list: decode: %w", err)
		}
		printExecutions(cmd, execs)
		return nil
	},
}

var executionMetaCmd = &cobra.Command{
	Use:   "metadata <execution-id>",
	Short: "Show metadata output for a single execution",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/executions/" + url.PathEscape(args[0]) + "/metadata")
		if err != nil {
			return fmt.Errorf("execution metadata: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var meta map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
			return fmt.Errorf("execution metadata: decode: %w", err)
		}
		if len(meta) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "(no metadata)")
			return nil
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KEY\tVALUE")
		for k, v := range meta {
			// Pretty-print JSON values if possible
			var parsed any
			if json.Unmarshal([]byte(v), &parsed) == nil {
				pretty, _ := json.MarshalIndent(parsed, "\t", "  ")
				fmt.Fprintf(w, "%s\t%s\n", k, string(pretty))
			} else {
				fmt.Fprintf(w, "%s\t%s\n", k, v)
			}
		}
		return w.Flush()
	},
}

// printExecutions writes a tabwriter table of executions to cmd output.
func printExecutions(cmd *cobra.Command, execs []map[string]any) {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tACTION_ID\tTWEET_ID\tSTATUS\tEXECUTED_AT")
	for _, e := range execs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			formatField(e["id"]),
			formatField(e["action_id"]),
			formatField(e["tweet_id"]),
			formatField(e["status"]),
			formatField(e["executed_at"]),
		)
	}
	_ = w.Flush()
}

func init() {
	executionListCmd.Flags().StringVar(&execActionID, "action-id", "", "Filter by action ID")
	executionListCmd.Flags().StringVar(&execTweetID, "tweet-id", "", "Filter by tweet ID")
	executionListCmd.Flags().StringVar(&execStatus, "status", "", "Filter by status (success, error)")
	executionListCmd.Flags().IntVar(&execPage, "page", 0, "Page number")
	executionListCmd.Flags().IntVar(&execPerPage, "per-page", 0, "Results per page")

	executionCmd.AddCommand(executionListCmd)
	executionCmd.AddCommand(executionMetaCmd)
	rootCmd.AddCommand(executionCmd)
}
