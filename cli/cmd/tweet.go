package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var tweetCmd = &cobra.Command{
	Use:   "tweet",
	Short: "Manage and query tweets",
}

var (
	tweetSourceID string
	tweetFrom     string
	tweetTo       string
	tweetPage     int
	tweetPerPage  int
)

var tweetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tweets",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := url.Values{}
		if tweetSourceID != "" {
			params.Set("source_id", tweetSourceID)
		}
		if tweetFrom != "" {
			params.Set("from", tweetFrom)
		}
		if tweetTo != "" {
			params.Set("to", tweetTo)
		}
		if tweetPage > 0 {
			params.Set("page", fmt.Sprintf("%d", tweetPage))
		}
		if tweetPerPage > 0 {
			params.Set("per_page", fmt.Sprintf("%d", tweetPerPage))
		}

		reqURL := baseURL + "/api/tweets"
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}

		resp, err := http.Get(reqURL)
		if err != nil {
			return fmt.Errorf("tweet list: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var tweets []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&tweets); err != nil {
			return fmt.Errorf("tweet list: decode response: %w", err)
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tAUTHOR\tCREATED_AT\tTEXT")
		for _, t := range tweets {
			id := formatField(t["id"])
			author := formatField(t["author_id"])
			createdAt := formatField(t["created_at"])
			text := formatField(t["text"])
			if len(text) > 60 {
				text = text[:57] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, author, createdAt, text)
		}
		return w.Flush()
	},
}

var tweetShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a tweet by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		resp, err := http.Get(baseURL + "/api/tweets/" + id)
		if err != nil {
			return fmt.Errorf("tweet show: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var tweet map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&tweet); err != nil {
			return fmt.Errorf("tweet show: decode response: %w", err)
		}

		data, err := json.MarshalIndent(tweet, "", "  ")
		if err != nil {
			return fmt.Errorf("tweet show: marshal output: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

var (
	reprocessTweetIDs  string
	reprocessModelNames string
)

var tweetReprocessCmd = &cobra.Command{
	Use:   "reprocess",
	Short: "Reprocess tweets through models",
	RunE: func(cmd *cobra.Command, args []string) error {
		var tweetIDs []string
		if reprocessTweetIDs != "" {
			tweetIDs = strings.Split(reprocessTweetIDs, ",")
			for i, id := range tweetIDs {
				tweetIDs[i] = strings.TrimSpace(id)
			}
		}

		var modelNames []string
		if reprocessModelNames != "" {
			modelNames = strings.Split(reprocessModelNames, ",")
			for i, name := range modelNames {
				modelNames[i] = strings.TrimSpace(name)
			}
		}

		body := map[string]interface{}{
			"tweet_ids":   tweetIDs,
			"model_names": modelNames,
		}
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("tweet reprocess: marshal body: %w", err)
		}

		resp, err := http.Post(baseURL+"/api/tweets/reprocess", "application/json", bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("tweet reprocess: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Reprocess request submitted")
		return nil
	},
}

func init() {
	tweetListCmd.Flags().StringVar(&tweetSourceID, "source-id", "", "Filter by source ID")
	tweetListCmd.Flags().StringVar(&tweetFrom, "from", "", "Filter tweets from this time (RFC3339)")
	tweetListCmd.Flags().StringVar(&tweetTo, "to", "", "Filter tweets to this time (RFC3339)")
	tweetListCmd.Flags().IntVar(&tweetPage, "page", 0, "Page number")
	tweetListCmd.Flags().IntVar(&tweetPerPage, "per-page", 0, "Tweets per page")

	tweetReprocessCmd.Flags().StringVar(&reprocessTweetIDs, "tweet-ids", "", "Comma-separated tweet IDs to reprocess")
	tweetReprocessCmd.Flags().StringVar(&reprocessModelNames, "model-names", "", "Comma-separated model names to use for reprocessing")

	tweetCmd.AddCommand(tweetListCmd)
	tweetCmd.AddCommand(tweetShowCmd)
	tweetCmd.AddCommand(tweetReprocessCmd)
	rootCmd.AddCommand(tweetCmd)
}
