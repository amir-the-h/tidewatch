package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var xTermCmd = &cobra.Command{
	Use:   "term",
	Short: "Manage X rule terms (keywords, hashtags, accounts…) that rules combine",
}

var xTermListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all X terms",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/x/terms")
		if err != nil {
			return fmt.Errorf("x term list: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var sources []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&sources); err != nil {
			return fmt.Errorf("x term list: decode response: %w", err)
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tCLASS\tVALUE\tACTIVE")
		for _, s := range sources {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				formatField(s["id"]),
				formatField(s["name"]),
				formatField(s["type"]),
				formatField(s["class"]),
				formatField(s["value"]),
				formatField(s["active"]),
			)
		}
		return w.Flush()
	},
}

var (
	xTermName  string
	xTermType  string
	xTermValue string
)

var xTermAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add an X term",
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]interface{}{
			"type":  xTermType,
			"value": xTermValue,
		}
		if xTermName != "" {
			body["name"] = xTermName
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("x term add: %w", err)
		}

		resp, err := apiPost(baseURL+"/api/x/terms", r)
		if err != nil {
			return fmt.Errorf("x term add: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var created map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("x term add: decode response: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Term added: id=%s name=%s type=%s value=%s\n",
			formatField(created["id"]),
			formatField(created["name"]),
			formatField(created["type"]),
			formatField(created["value"]),
		)
		return nil
	},
}

var xTermRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove an X term by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		resp, err := apiDelete(baseURL + "/api/x/terms/" + id)
		if err != nil {
			return fmt.Errorf("x term remove: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Term %s removed\n", id)
		return nil
	},
}

func init() {
	xTermAddCmd.Flags().StringVar(&xTermName, "name", "", "Human-readable label (defaults to value)")
	xTermAddCmd.Flags().StringVar(&xTermType, "type", "", "Term type (account|keyword|hashtag|phrase|mention|...)")
	xTermAddCmd.Flags().StringVar(&xTermValue, "value", "", "Term value (e.g. @handle or keyword)")
	_ = xTermAddCmd.MarkFlagRequired("type")
	_ = xTermAddCmd.MarkFlagRequired("value")

	xTermCmd.AddCommand(xTermListCmd)
	xTermCmd.AddCommand(xTermAddCmd)
	xTermCmd.AddCommand(xTermRemoveCmd)
	xCmd.AddCommand(xTermCmd)
}
