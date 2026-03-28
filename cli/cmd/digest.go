package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var digestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Manage LLM digest jobs",
}

var digestListCmd = &cobra.Command{
	Use:   "list",
	Short: "List digest jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/digest")
		if err != nil {
			return fmt.Errorf("digest list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var jobs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
			return fmt.Errorf("digest list: decode: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATUS\tTWEET_COUNT\tCREATED_AT")
		for _, j := range jobs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				formatField(j["id"]),
				formatField(j["status"]),
				formatField(j["tweet_count"]),
				formatField(j["created_at"]),
			)
		}
		return w.Flush()
	},
}

var digestShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a digest job and its output",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/digest/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("digest show: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var job map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
			return fmt.Errorf("digest show: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "ID:          %s\nStatus:      %s\nTweet Count: %s\nCreated:     %s\nCompleted:   %s\n\n",
			formatField(job["id"]),
			formatField(job["status"]),
			formatField(job["tweet_count"]),
			formatField(job["created_at"]),
			formatField(job["completed_at"]),
		)
		if output, ok := job["output"].(string); ok && output != "" {
			fmt.Fprintln(cmd.OutOrStdout(), "--- Output ---")
			fmt.Fprintln(cmd.OutOrStdout(), output)
		}
		if errMsg, ok := job["error"].(string); ok && errMsg != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Error: %s\n", errMsg)
		}
		return nil
	},
}

var digestDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a digest job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiDelete(baseURL + "/api/digest/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("digest delete: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Digest %s deleted\n", args[0])
		return nil
	},
}

func init() {
	digestCmd.AddCommand(digestListCmd)
	digestCmd.AddCommand(digestShowCmd)
	digestCmd.AddCommand(digestDeleteCmd)
	rootCmd.AddCommand(digestCmd)
}
