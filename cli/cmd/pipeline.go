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

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Manage inference pipelines (onnx, ollama, internal)",
}

var (
	pipelineTypeFilter   string
	pipelineActiveFilter string
	pipelineName         string
	pipelineType         string
	pipelineConfig       string
)

var pipelineListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pipelines",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := url.Values{}
		if pipelineTypeFilter != "" {
			params.Set("type", pipelineTypeFilter)
		}
		if pipelineActiveFilter != "" {
			params.Set("active", pipelineActiveFilter)
		}
		reqURL := baseURL + "/api/pipelines"
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}
		resp, err := apiGet(reqURL)
		if err != nil {
			return fmt.Errorf("pipeline list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var pipelines []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&pipelines); err != nil {
			return fmt.Errorf("pipeline list: decode: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tACTIVE")
		for _, p := range pipelines {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				formatField(p["id"]),
				formatField(p["name"]),
				formatField(p["type"]),
				formatField(p["active"]),
			)
		}
		return w.Flush()
	},
}

var pipelineShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show pipeline details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/pipelines/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("pipeline show: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var p map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			return fmt.Errorf("pipeline show: decode: %w", err)
		}
		data, _ := json.MarshalIndent(p, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

var pipelineAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new pipeline",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{"name": args[0], "type": pipelineType}
		if pipelineConfig != "" {
			body["config"] = json.RawMessage(pipelineConfig)
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("pipeline add: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/pipelines", r)
		if err != nil {
			return fmt.Errorf("pipeline add: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var created map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("pipeline add: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Pipeline created: id=%s name=%s type=%s\n",
			formatField(created["id"]),
			formatField(created["name"]),
			formatField(created["type"]),
		)
		return nil
	},
}

var pipelineUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a pipeline's name or config",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if cmd.Flags().Changed("name") {
			body["name"] = pipelineName
		}
		if cmd.Flags().Changed("config") {
			body["config"] = json.RawMessage(pipelineConfig)
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("pipeline update: %w", err)
		}
		resp, err := apiPut(baseURL+"/api/pipelines/"+url.PathEscape(args[0]), r)
		if err != nil {
			return fmt.Errorf("pipeline update: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Pipeline %s updated\n", args[0])
		return nil
	},
}

var pipelineRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Delete a pipeline",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiDelete(baseURL + "/api/pipelines/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("pipeline remove: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Pipeline %s removed\n", args[0])
		return nil
	},
}

var pipelineActivateCmd = &cobra.Command{
	Use:   "activate <id>",
	Short: "Activate a pipeline",
	Args:  cobra.ExactArgs(1),
	RunE:  pipelineLifecycle("activate"),
}

var pipelineDeactivateCmd = &cobra.Command{
	Use:   "deactivate <id>",
	Short: "Deactivate a pipeline",
	Args:  cobra.ExactArgs(1),
	RunE:  pipelineLifecycle("deactivate"),
}

func pipelineLifecycle(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		resp, err := apiPost(baseURL+"/api/pipelines/"+url.PathEscape(args[0])+"/"+action, nil)
		if err != nil {
			return fmt.Errorf("pipeline %s: %w", action, err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Pipeline %s %sd\n", args[0], action)
		return nil
	}
}

var pipelineHandlersCmd = &cobra.Command{
	Use:   "handlers",
	Short: "List available internal pipeline handlers",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/pipelines/handlers")
		if err != nil {
			return fmt.Errorf("pipeline handlers: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var handlers []string
		if err := json.NewDecoder(resp.Body).Decode(&handlers); err != nil {
			return fmt.Errorf("pipeline handlers: decode: %w", err)
		}
		for _, h := range handlers {
			fmt.Fprintln(cmd.OutOrStdout(), h)
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// pipeline backends
// ---------------------------------------------------------------------------

var pipelineBackendsCmd = &cobra.Command{
	Use:   "backends",
	Short: "Manage pipeline backend assignments",
}

var pipelineBackendsListCmd = &cobra.Command{
	Use:   "list <pipeline-id>",
	Short: "List backends assigned to a pipeline in priority order",
	Args:  cobra.ExactArgs(1),
	RunE:  runPipelineBackendsList,
}

var pipelineBackendsSetCmd = &cobra.Command{
	Use:   "set <pipeline-id> <backend-id>:<priority> [<backend-id>:<priority> ...]",
	Short: "Replace the full backend assignment list for a pipeline",
	Long: `Replaces all backend assignments for the pipeline atomically.
Each argument is backend-id:priority, e.g.: set 3 1:1 2:2`,
	Args: cobra.MinimumNArgs(2),
	RunE: runPipelineBackendsSet,
}

var pipelineBackendsUnassignCmd = &cobra.Command{
	Use:   "unassign <pipeline-id> <backend-id>",
	Short: "Remove one backend from a pipeline's assignment list",
	Args:  cobra.ExactArgs(2),
	RunE:  runPipelineBackendsUnassign,
}

func runPipelineBackendsList(cmd *cobra.Command, args []string) error {
	resp, err := apiGet(baseURL + "/api/pipelines/" + url.PathEscape(args[0]) + "/backends")
	if err != nil {
		return fmt.Errorf("pipeline backends list: %w", err)
	}
	defer resp.Body.Close()
	if err := handleAPIError(cmd, resp); err != nil {
		return err
	}
	var pbs []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pbs); err != nil {
		return fmt.Errorf("pipeline backends list: decode: %w", err)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PIPELINE_ID\tBACKEND_ID\tPRIORITY")
	for _, pb := range pbs {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			formatField(pb["pipeline_id"]),
			formatField(pb["backend_id"]),
			formatField(pb["priority"]),
		)
	}
	return w.Flush()
}

func runPipelineBackendsSet(cmd *cobra.Command, args []string) error {
	type assignment struct {
		BackendID int64 `json:"backend_id"`
		Priority  int   `json:"priority"`
	}
	pipelineID := args[0]
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
		return fmt.Errorf("pipeline backends set: %w", err)
	}
	resp, err := apiPut(baseURL+"/api/pipelines/"+url.PathEscape(pipelineID)+"/backends", body)
	if err != nil {
		return fmt.Errorf("pipeline backends set: %w", err)
	}
	defer resp.Body.Close()
	if err := handleAPIError(cmd, resp); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Pipeline %s backend assignments updated\n", pipelineID)
	return nil
}

func runPipelineBackendsUnassign(cmd *cobra.Command, args []string) error {
	resp, err := apiDelete(baseURL + "/api/pipelines/" + url.PathEscape(args[0]) + "/backends/" + url.PathEscape(args[1]))
	if err != nil {
		return fmt.Errorf("pipeline backends unassign: %w", err)
	}
	defer resp.Body.Close()
	if err := handleAPIError(cmd, resp); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Backend %s removed from pipeline %s\n", args[1], args[0])
	return nil
}

func init() {
	pipelineListCmd.Flags().StringVar(&pipelineTypeFilter, "type", "", "Filter by type (onnx, ollama, internal)")
	pipelineListCmd.Flags().StringVar(&pipelineActiveFilter, "active", "", "Filter by active state (true/false)")

	pipelineAddCmd.Flags().StringVar(&pipelineType, "type", "", "Pipeline type: onnx, ollama, internal")
	pipelineAddCmd.Flags().StringVar(&pipelineConfig, "config", "", "Config JSON string")
	_ = pipelineAddCmd.MarkFlagRequired("type")

	pipelineUpdateCmd.Flags().StringVar(&pipelineName, "name", "", "New name")
	pipelineUpdateCmd.Flags().StringVar(&pipelineConfig, "config", "", "New config JSON string")

	pipelineBackendsCmd.AddCommand(pipelineBackendsListCmd, pipelineBackendsSetCmd, pipelineBackendsUnassignCmd)

	pipelineCmd.AddCommand(pipelineListCmd)
	pipelineCmd.AddCommand(pipelineShowCmd)
	pipelineCmd.AddCommand(pipelineAddCmd)
	pipelineCmd.AddCommand(pipelineUpdateCmd)
	pipelineCmd.AddCommand(pipelineRemoveCmd)
	pipelineCmd.AddCommand(pipelineActivateCmd)
	pipelineCmd.AddCommand(pipelineDeactivateCmd)
	pipelineCmd.AddCommand(pipelineHandlersCmd)
	pipelineCmd.AddCommand(pipelineBackendsCmd)
	rootCmd.AddCommand(pipelineCmd)
}
