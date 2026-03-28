package cmd

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var failureCmd = &cobra.Command{
	Use:   "failure",
	Short: "Inspect and manage the dead-letter queue",
}

var failureListCmd = &cobra.Command{
	Use:   "list",
	Short: "List failed tasks in the dead-letter queue",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/failures")
		if err != nil {
			return fmt.Errorf("failure list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var failures []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&failures); err != nil {
			return fmt.Errorf("failure list: decode: %w", err)
		}
		if len(failures) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No failures")
			return nil
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TRIGGER\tACTION_ID\tERROR")
		for _, f := range failures {
			fmt.Fprintf(w, "%s\t%s\t%s\n",
				formatField(f["trigger_type"]),
				formatField(f["action_id"]),
				formatField(f["error"]),
			)
		}
		return w.Flush()
	},
}

var failureClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all entries from the dead-letter queue",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiDelete(baseURL + "/api/failures")
		if err != nil {
			return fmt.Errorf("failure clear: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Dead-letter queue cleared")
		return nil
	},
}

var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Manage the Redis task queue",
}

var queueFlushCmd = &cobra.Command{
	Use:   "flush",
	Short: "Flush all pending tasks from the Redis queue",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiDelete(baseURL + "/api/queue")
		if err != nil {
			return fmt.Errorf("queue flush: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Task queue flushed")
		return nil
	},
}

func init() {
	failureCmd.AddCommand(failureListCmd)
	failureCmd.AddCommand(failureClearCmd)
	rootCmd.AddCommand(failureCmd)

	queueCmd.AddCommand(queueFlushCmd)
	rootCmd.AddCommand(queueCmd)
}
