package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var signalCmd = &cobra.Command{
	Use:   "signal",
	Short: "Manage and query signals (ingested posts and articles)",
}

var (
	signalRuleID  string
	signalKind    string
	signalFrom    string
	signalTo      string
	signalPage    int
	signalPerPage int
)

var signalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List signals, newest first",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := url.Values{}
		if signalRuleID != "" {
			params.Set("rule_id", signalRuleID)
		}
		if signalKind != "" {
			params.Set("kind", signalKind)
		}
		if signalFrom != "" {
			params.Set("from", signalFrom)
		}
		if signalTo != "" {
			params.Set("to", signalTo)
		}
		if signalPage > 0 {
			params.Set("page", fmt.Sprintf("%d", signalPage))
		}
		if signalPerPage > 0 {
			params.Set("per_page", fmt.Sprintf("%d", signalPerPage))
		}

		reqURL := baseURL + "/api/signals"
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}

		resp, err := apiGet(reqURL)
		if err != nil {
			return fmt.Errorf("signal list: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var result struct {
			Signals []map[string]interface{} `json:"signals"`
			Total   int                      `json:"total"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("signal list: decode response: %w", err)
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tKIND\tAUTHOR\tPUBLISHED_AT\tTEXT")
		for _, s := range result.Signals {
			text := formatField(s["title"])
			if text == "" || text == "-" {
				text = formatField(s["text"])
			}
			if r := []rune(text); len(r) > 60 {
				text = string(r[:57]) + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				formatField(s["id"]), formatField(s["kind"]), formatField(s["author"]),
				formatField(s["published_at"]), text)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\n%d of %d signals\n", len(result.Signals), result.Total)
		return nil
	},
}

var signalShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a signal and its action executions",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		resp, err := apiGet(baseURL + "/api/signals/" + id)
		if err != nil {
			return fmt.Errorf("signal show: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var signal map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&signal); err != nil {
			return fmt.Errorf("signal show: decode response: %w", err)
		}

		data, err := json.MarshalIndent(signal, "", "  ")
		if err != nil {
			return fmt.Errorf("signal show: marshal output: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

var (
	reprocessSignalIDs string
	reprocessActionIDs string
)

var signalReprocessCmd = &cobra.Command{
	Use:   "reprocess",
	Short: "Re-queue signals through the new_signal pipelines, or through specific actions",
	RunE: func(cmd *cobra.Command, args []string) error {
		signalIDs, err := parseIDList(reprocessSignalIDs)
		if err != nil {
			return fmt.Errorf("signal reprocess: --signal-ids: %w", err)
		}
		if len(signalIDs) == 0 {
			return fmt.Errorf("signal reprocess: --signal-ids is required")
		}
		actionIDs, err := parseIDList(reprocessActionIDs)
		if err != nil {
			return fmt.Errorf("signal reprocess: --action-ids: %w", err)
		}

		r, err := jsonBody(map[string]interface{}{
			"signal_ids": signalIDs,
			"action_ids": actionIDs,
		})
		if err != nil {
			return fmt.Errorf("signal reprocess: %w", err)
		}

		resp, err := apiPost(baseURL+"/api/signals/reprocess", r)
		if err != nil {
			return fmt.Errorf("signal reprocess: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var result struct {
			Requeued int `json:"requeued"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(cmd.OutOrStdout(), "Requeued %d signals\n", result.Requeued)
		return nil
	},
}

var deleteSignalIDs string

var signalDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete signals with their executions, metadata and entity tracks",
	RunE: func(cmd *cobra.Command, args []string) error {
		ids, err := parseIDList(deleteSignalIDs)
		if err != nil {
			return fmt.Errorf("signal delete: --ids: %w", err)
		}
		if len(ids) == 0 {
			return fmt.Errorf("signal delete: --ids is required")
		}
		r, err := jsonBody(map[string]interface{}{"ids": ids})
		if err != nil {
			return fmt.Errorf("signal delete: %w", err)
		}
		resp, err := apiRequest(http.MethodDelete, baseURL+"/api/signals", r)
		if err != nil {
			return fmt.Errorf("signal delete: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var result struct {
			Deleted int64 `json:"deleted"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d signals\n", result.Deleted)
		return nil
	},
}

// parseIDList parses a comma-separated list of integer IDs; empty input gives nil.
func parseIDList(s string) ([]int64, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ID %q", p)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func init() {
	signalListCmd.Flags().StringVar(&signalRuleID, "rule-id", "", "Filter by X rule ID")
	signalListCmd.Flags().StringVar(&signalKind, "kind", "", "Filter by kind: post or article")
	signalListCmd.Flags().StringVar(&signalFrom, "from", "", "Filter signals published from this time (RFC3339)")
	signalListCmd.Flags().StringVar(&signalTo, "to", "", "Filter signals published up to this time (RFC3339)")
	signalListCmd.Flags().IntVar(&signalPage, "page", 0, "Page number")
	signalListCmd.Flags().IntVar(&signalPerPage, "per-page", 0, "Signals per page")

	signalReprocessCmd.Flags().StringVar(&reprocessSignalIDs, "signal-ids", "", "Comma-separated signal IDs to reprocess (required)")
	signalReprocessCmd.Flags().StringVar(&reprocessActionIDs, "action-ids", "", "Comma-separated action IDs to run instead of the new_signal pipelines")

	signalDeleteCmd.Flags().StringVar(&deleteSignalIDs, "ids", "", "Comma-separated signal IDs to delete (required)")

	signalCmd.AddCommand(signalListCmd)
	signalCmd.AddCommand(signalShowCmd)
	signalCmd.AddCommand(signalReprocessCmd)
	signalCmd.AddCommand(signalDeleteCmd)
	rootCmd.AddCommand(signalCmd)
}
