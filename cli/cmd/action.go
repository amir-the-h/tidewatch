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
	Short: "Manage execution-unit actions (onnx, ollama, internal, notification)",
}

var (
	actionTypeFilter string
	actionActiveFilt string
	actionName       string
	actionType       string
	actionConfig     string
)

var actionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List actions",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := url.Values{}
		if actionTypeFilter != "" {
			params.Set("type", actionTypeFilter)
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
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tACTIVE")
		for _, a := range actions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				formatField(a["id"]),
				formatField(a["name"]),
				formatField(a["type"]),
				formatField(a["active"]),
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
		body := map[string]any{"name": args[0], "type": actionType}
		if actionConfig != "" {
			body["config"] = json.RawMessage(actionConfig)
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("action add: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/actions", r)
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
		fmt.Fprintf(cmd.OutOrStdout(), "Action created: id=%s name=%s type=%s\n",
			formatField(created["id"]),
			formatField(created["name"]),
			formatField(created["type"]),
		)
		return nil
	},
}

var (
	actionUpdateName   string
	actionUpdateConfig string
)

var actionUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an action's name or config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if cmd.Flags().Changed("name") {
			body["name"] = actionUpdateName
		}
		if cmd.Flags().Changed("config") {
			body["config"] = json.RawMessage(actionUpdateConfig)
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

var actionHandlersCmd = &cobra.Command{
	Use:   "handlers",
	Short: "List available internal action handler names",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/actions/handlers")
		if err != nil {
			return fmt.Errorf("action handlers: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var handlers []string
		if err := json.NewDecoder(resp.Body).Decode(&handlers); err != nil {
			return fmt.Errorf("action handlers: decode: %w", err)
		}
		for _, h := range handlers {
			fmt.Fprintln(cmd.OutOrStdout(), h)
		}
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

// ---------------------------------------------------------------------------
// action backends
// ---------------------------------------------------------------------------

var actionBackendsCmd = &cobra.Command{
	Use:   "backends",
	Short: "Manage action backend assignments",
}

var actionBackendsListCmd = &cobra.Command{
	Use:   "list <action-id>",
	Short: "List backends assigned to an action in priority order",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/actions/" + url.PathEscape(args[0]) + "/backends")
		if err != nil {
			return fmt.Errorf("action backends list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var abs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&abs); err != nil {
			return fmt.Errorf("action backends list: decode: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ACTION_ID\tBACKEND_ID\tPRIORITY")
		for _, ab := range abs {
			fmt.Fprintf(w, "%s\t%s\t%s\n",
				formatField(ab["action_id"]),
				formatField(ab["backend_id"]),
				formatField(ab["priority"]),
			)
		}
		return w.Flush()
	},
}

var actionBackendsSetCmd = &cobra.Command{
	Use:   "set <action-id> <backend-id>:<priority> [<backend-id>:<priority> ...]",
	Short: "Replace the full backend assignment list for an action",
	Long: `Replaces all backend assignments for the action atomically.
Each argument is backend-id:priority, e.g.: set 3 1:1 2:2`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		type assignment struct {
			BackendID int64 `json:"backend_id"`
			Priority  int   `json:"priority"`
		}
		actionID := args[0]
		assignments := make([]assignment, 0, len(args)-1)
		for _, pair := range args[1:] {
			parts := strings.SplitN(pair, ":", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid pair %q: expected backend-id:priority", pair)
			}
			bid, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid backend-id %q: %w", parts[0], err)
			}
			pri, err := strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("invalid priority %q: %w", parts[1], err)
			}
			assignments = append(assignments, assignment{BackendID: bid, Priority: pri})
		}
		body, err := jsonBody(assignments)
		if err != nil {
			return fmt.Errorf("action backends set: %w", err)
		}
		resp, err := apiPut(baseURL+"/api/actions/"+url.PathEscape(actionID)+"/backends", body)
		if err != nil {
			return fmt.Errorf("action backends set: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Action %s backend assignments updated\n", actionID)
		return nil
	},
}

var actionBackendsUnassignCmd = &cobra.Command{
	Use:   "unassign <action-id> <backend-id>",
	Short: "Remove one backend from an action's assignment list",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiDelete(baseURL + "/api/actions/" + url.PathEscape(args[0]) + "/backends/" + url.PathEscape(args[1]))
		if err != nil {
			return fmt.Errorf("action backends unassign: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Backend %s removed from action %s\n", args[1], args[0])
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
	actionListCmd.Flags().StringVar(&actionTypeFilter, "type", "", "Filter by type (onnx, ollama, internal, notification)")
	actionListCmd.Flags().StringVar(&actionActiveFilt, "active", "", "Filter by active state (true/false)")

	actionAddCmd.Flags().StringVar(&actionType, "type", "", "Action type: onnx, ollama, internal, notification")
	actionAddCmd.Flags().StringVar(&actionConfig, "config", "", "Config JSON string")
	_ = actionAddCmd.MarkFlagRequired("type")

	actionUpdateCmd.Flags().StringVar(&actionUpdateName, "name", "", "New name")
	actionUpdateCmd.Flags().StringVar(&actionUpdateConfig, "config", "", "New config JSON string")

	actionBackendsCmd.AddCommand(actionBackendsListCmd, actionBackendsSetCmd, actionBackendsUnassignCmd)

	actionCmd.AddCommand(actionListCmd)
	actionCmd.AddCommand(actionShowCmd)
	actionCmd.AddCommand(actionAddCmd)
	actionCmd.AddCommand(actionUpdateCmd)
	actionCmd.AddCommand(actionRemoveCmd)
	actionCmd.AddCommand(actionActivateCmd)
	actionCmd.AddCommand(actionDeactivateCmd)
	actionCmd.AddCommand(actionHandlersCmd)
	actionCmd.AddCommand(actionExecutionsCmd)
	actionCmd.AddCommand(actionBackendsCmd)
	rootCmd.AddCommand(actionCmd)
}
