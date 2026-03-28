package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var ruleCmd = &cobra.Command{
	Use:   "rule",
	Short: "Manage stream filter rules",
}

var ruleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all rules",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/rules")
		if err != nil {
			return fmt.Errorf("rule list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var rules []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
			return fmt.Errorf("rule list: decode: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tACTIVE\tX_RULE_ID\tCOMPILED")
		for _, r := range rules {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				formatField(r["id"]),
				formatField(r["name"]),
				formatField(r["active"]),
				formatField(r["x_rule_id"]),
				formatField(r["compiled_value"]),
			)
		}
		return w.Flush()
	},
}

var ruleShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a rule with expression and compiled syntax",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		resp, err := apiGet(baseURL + "/api/rules/" + id)
		if err != nil {
			return fmt.Errorf("rule show: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var rule map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&rule); err != nil {
			return fmt.Errorf("rule show: decode: %w", err)
		}
		exprBytes, _ := json.MarshalIndent(rule["expression"], "  ", "  ")
		fmt.Fprintf(cmd.OutOrStdout(), "Name:     %s\nActive:   %s\nX Rule:   %s\n\nExpression:\n  %s\n\nCompiled:\n  %s\n",
			formatField(rule["name"]),
			formatField(rule["active"]),
			formatField(rule["x_rule_id"]),
			string(exprBytes),
			formatField(rule["compiled_value"]),
		)
		return nil
	},
}

var (
	ruleExprStr  string
	ruleExprFile string
	ruleName     string
)

func parseRuleExpr(exprStr, exprFile string) (json.RawMessage, error) {
	if exprStr != "" {
		return json.RawMessage(exprStr), nil
	}
	if exprFile != "" {
		data, err := os.ReadFile(exprFile)
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
		return json.RawMessage(data), nil
	}
	return nil, fmt.Errorf("one of --expr or --file is required")
}

var ruleAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new rule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		expr, err := parseRuleExpr(ruleExprStr, ruleExprFile)
		if err != nil {
			return fmt.Errorf("rule add: %w", err)
		}
		body := map[string]interface{}{
			"name":       args[0],
			"expression": expr,
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("rule add: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/rules", r)
		if err != nil {
			return fmt.Errorf("rule add: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var created map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("rule add: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Rule created: id=%s name=%s compiled=%s\n",
			formatField(created["id"]),
			formatField(created["name"]),
			formatField(created["compiled_value"]),
		)
		return nil
	},
}

var ruleUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a rule's name or expression",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		body := map[string]interface{}{}
		if ruleName != "" {
			body["name"] = ruleName
		}
		if ruleExprStr != "" || ruleExprFile != "" {
			expr, err := parseRuleExpr(ruleExprStr, ruleExprFile)
			if err != nil {
				return fmt.Errorf("rule update: %w", err)
			}
			body["expression"] = expr
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("rule update: %w", err)
		}
		resp, err := apiPut(baseURL+"/api/rules/"+id, r)
		if err != nil {
			return fmt.Errorf("rule update: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Rule %s updated\n", id)
		return nil
	},
}

var ruleRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Delete a rule (only if it has no tweet history)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		resp, err := apiDelete(baseURL + "/api/rules/" + id)
		if err != nil {
			return fmt.Errorf("rule remove: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Rule %s removed\n", id)
		return nil
	},
}

var ruleActivateCmd = &cobra.Command{
	Use:   "activate <id>",
	Short: "Activate a rule (push to X)",
	Args:  cobra.ExactArgs(1),
	RunE:  ruleLifecycleAction("activate"),
}

var ruleDeactivateCmd = &cobra.Command{
	Use:   "deactivate <id>",
	Short: "Deactivate a rule (remove from X, keep locally)",
	Args:  cobra.ExactArgs(1),
	RunE:  ruleLifecycleAction("deactivate"),
}

func ruleLifecycleAction(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		resp, err := apiPost(baseURL+"/api/rules/"+id+"/"+action, nil)
		if err != nil {
			return fmt.Errorf("rule %s: %w", action, err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Rule %s %sd\n", id, action)
		return nil
	}
}

func init() {
	ruleAddCmd.Flags().StringVar(&ruleExprStr, "expr", "", "Expression JSON string")
	ruleAddCmd.Flags().StringVar(&ruleExprFile, "file", "", "Path to expression JSON file")

	ruleUpdateCmd.Flags().StringVar(&ruleName, "name", "", "New name")
	ruleUpdateCmd.Flags().StringVar(&ruleExprStr, "expr", "", "New expression JSON string")
	ruleUpdateCmd.Flags().StringVar(&ruleExprFile, "file", "", "Path to new expression JSON file")

	ruleCmd.AddCommand(ruleListCmd)
	ruleCmd.AddCommand(ruleShowCmd)
	ruleCmd.AddCommand(ruleAddCmd)
	ruleCmd.AddCommand(ruleUpdateCmd)
	ruleCmd.AddCommand(ruleRemoveCmd)
	ruleCmd.AddCommand(ruleActivateCmd)
	ruleCmd.AddCommand(ruleDeactivateCmd)
	rootCmd.AddCommand(ruleCmd)
}
