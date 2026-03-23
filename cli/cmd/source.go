package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Manage stream sources",
}

var sourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Get(baseURL + "/api/sources")
		if err != nil {
			return fmt.Errorf("source list: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var sources []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&sources); err != nil {
			return fmt.Errorf("source list: decode response: %w", err)
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tTYPE\tVALUE\tCREATED_AT")
		for _, s := range sources {
			id := formatField(s["id"])
			typ := formatField(s["type"])
			value := formatField(s["value"])
			createdAt := formatField(s["created_at"])
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, typ, value, createdAt)
		}
		return w.Flush()
	},
}

var (
	sourceType  string
	sourceValue string
)

var sourceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new source",
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]string{
			"type":  sourceType,
			"value": sourceValue,
		}
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("source add: marshal body: %w", err)
		}

		resp, err := http.Post(baseURL+"/api/sources", "application/json", bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("source add: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var created map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("source add: decode response: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Source added: id=%s type=%s value=%s\n",
			formatField(created["id"]),
			formatField(created["type"]),
			formatField(created["value"]),
		)
		return nil
	},
}

var sourceRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a source by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodDelete, baseURL+"/api/sources/"+id, nil)
		if err != nil {
			return fmt.Errorf("source remove: build request: %w", err)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("source remove: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Source %s removed\n", id)
		return nil
	},
}

func init() {
	sourceAddCmd.Flags().StringVar(&sourceType, "type", "", "Source type (account|keyword)")
	sourceAddCmd.Flags().StringVar(&sourceValue, "value", "", "Source value (e.g. @handle or keyword)")
	_ = sourceAddCmd.MarkFlagRequired("type")
	_ = sourceAddCmd.MarkFlagRequired("value")

	sourceCmd.AddCommand(sourceListCmd)
	sourceCmd.AddCommand(sourceAddCmd)
	sourceCmd.AddCommand(sourceRemoveCmd)
	rootCmd.AddCommand(sourceCmd)
}

// formatField converts an interface{} to a string for display.
func formatField(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case float64:
		return fmt.Sprintf("%.0f", val)
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}
