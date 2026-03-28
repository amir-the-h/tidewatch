package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var actionCmd = &cobra.Command{
	Use:   "action",
	Short: "Manage event-driven actions",
}

var (
	actionTrigger    string
	actionPipelineID int64
	actionRuleIDs    string
	actionActiveFilt string
)

var actionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List actions",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := url.Values{}
		if actionTrigger != "" {
			params.Set("trigger_type", actionTrigger)
		}
		if actionActiveFilt != "" {
			params.Set("active", actionActiveFilt)
		}
		reqURL := baseURL + "/api/actions"
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}
		resp, err := apiGet(reqURL)
		if err != nil {
			return fmt.Errorf("action list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var actions []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&actions); err != nil {
			return fmt.Errorf("action list: decode: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTRIGGER\tPIPELINE_ID\tACTIVE\tRULE_IDS")
		for _, a := range actions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				formatField(a["id"]),
				formatField(a["name"]),
				formatField(a["trigger_type"]),
				formatField(a["pipeline_id"]),
				formatField(a["active"]),
				formatField(a["rule_ids"]),
			)
		}
		return w.Flush()
	},
}

var actionShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show action details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/actions/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("action show: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var a map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
			return fmt.Errorf("action show: decode: %w", err)
		}
		data, _ := json.MarshalIndent(a, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

var actionAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new action",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ruleIDs := parseIntList(actionRuleIDs)
		body, err := jsonBody(map[string]any{
			"name":         args[0],
			"trigger_type": actionTrigger,
			"pipeline_id":  actionPipelineID,
			"rule_ids":     ruleIDs,
		})
		if err != nil {
			return fmt.Errorf("action add: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/actions", body)
		if err != nil {
			return fmt.Errorf("action add: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var created map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("action add: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Action created: id=%s name=%s trigger=%s\n",
			formatField(created["id"]),
			formatField(created["name"]),
			formatField(created["trigger_type"]),
		)
		return nil
	},
}

var (
	actionUpdateName    string
	actionUpdateRuleIDs string
)

var actionUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an action's name or rule IDs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if cmd.Flags().Changed("name") {
			body["name"] = actionUpdateName
		}
		if cmd.Flags().Changed("rules") {
			body["rule_ids"] = parseIntList(actionUpdateRuleIDs)
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("action update: %w", err)
		}
		resp, err := apiPut(baseURL+"/api/actions/"+url.PathEscape(args[0]), r)
		if err != nil {
			return fmt.Errorf("action update: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Action %s updated\n", args[0])
		return nil
	},
}

var actionRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Delete an action",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiDelete(baseURL + "/api/actions/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("action remove: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Action %s removed\n", args[0])
		return nil
	},
}

var actionActivateCmd = &cobra.Command{
	Use:   "activate <id>",
	Short: "Activate an action",
	Args:  cobra.ExactArgs(1),
	RunE:  actionLifecycle("activate"),
}

var actionDeactivateCmd = &cobra.Command{
	Use:   "deactivate <id>",
	Short: "Deactivate an action",
	Args:  cobra.ExactArgs(1),
	RunE:  actionLifecycle("deactivate"),
}

func actionLifecycle(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		resp, err := apiPost(baseURL+"/api/actions/"+url.PathEscape(args[0])+"/"+action, nil)
		if err != nil {
			return fmt.Errorf("action %s: %w", action, err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Action %s %sd\n", args[0], action)
		return nil
	}
}

var actionRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Queue all matching tweets through an action",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiPost(baseURL+"/api/actions/"+url.PathEscape(args[0])+"/run", nil)
		if err != nil {
			return fmt.Errorf("action run: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("action run: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Queued %s tweets\n", formatField(result["queued"]))
		return nil
	},
}

var actionExecutionsCmd = &cobra.Command{
	Use:   "executions <id>",
	Short: "List executions for an action",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/actions/" + url.PathEscape(args[0]) + "/executions")
		if err != nil {
			return fmt.Errorf("action executions: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var execs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&execs); err != nil {
			return fmt.Errorf("action executions: decode: %w", err)
		}
		printExecutions(cmd, execs)
		return nil
	},
}

// parseIntList parses a comma-separated list of integers.
func parseIntList(s string) []int64 {
	if s == "" {
		return []int64{}
	}
	parts := strings.Split(s, ",")
	result := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if n, err := strconv.ParseInt(p, 10, 64); err == nil {
			result = append(result, n)
		}
	}
	return result
}

func init() {
	actionListCmd.Flags().StringVar(&actionTrigger, "trigger", "", "Filter by trigger type")
	actionListCmd.Flags().StringVar(&actionActiveFilt, "active", "", "Filter by active state (true/false)")

	actionAddCmd.Flags().StringVar(&actionTrigger, "trigger", "", "Trigger type (new_tweet, new_categorization, entity_matched)")
	actionAddCmd.Flags().Int64Var(&actionPipelineID, "pipeline", 0, "Pipeline ID")
	actionAddCmd.Flags().StringVar(&actionRuleIDs, "rules", "", "Comma-separated rule IDs (empty = all rules)")
	_ = actionAddCmd.MarkFlagRequired("trigger")
	_ = actionAddCmd.MarkFlagRequired("pipeline")

	actionUpdateCmd.Flags().StringVar(&actionUpdateName, "name", "", "New name")
	actionUpdateCmd.Flags().StringVar(&actionUpdateRuleIDs, "rules", "", "New comma-separated rule IDs")

	actionCmd.AddCommand(actionListCmd)
	actionCmd.AddCommand(actionShowCmd)
	actionCmd.AddCommand(actionAddCmd)
	actionCmd.AddCommand(actionUpdateCmd)
	actionCmd.AddCommand(actionRemoveCmd)
	actionCmd.AddCommand(actionActivateCmd)
	actionCmd.AddCommand(actionDeactivateCmd)
	actionCmd.AddCommand(actionRunCmd)
	actionCmd.AddCommand(actionExecutionsCmd)
	rootCmd.AddCommand(actionCmd)
}
