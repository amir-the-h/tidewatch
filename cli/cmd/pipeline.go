package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Manage DAG workflow pipelines",
}

var (
	pipelineName         string
	pipelineActiveFilter string
)

var pipelineListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pipelines",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/pipelines")
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
		fmt.Fprintln(w, "ID\tNAME\tACTIVE\tNODES")
		for _, p := range pipelines {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				formatField(p["id"]),
				formatField(p["name"]),
				formatField(p["active"]),
				formatField(p["node_count"]),
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
		body := map[string]any{"name": args[0]}
		if pipelineDescription != "" {
			body["description"] = pipelineDescription
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
		fmt.Fprintf(cmd.OutOrStdout(), "Pipeline created: id=%s name=%s\n",
			formatField(created["id"]),
			formatField(created["name"]),
		)
		return nil
	},
}

var (
	pipelineDescription string
	pipelineUpdateName  string
	pipelineUpdateDesc  string
)

var pipelineUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a pipeline's name or description",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if cmd.Flags().Changed("name") {
			body["name"] = pipelineUpdateName
		}
		if cmd.Flags().Changed("description") {
			body["description"] = pipelineUpdateDesc
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

var pipelineRunNewOnly bool

var pipelineRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Dispatch historical signals through a pipeline",
	Long:  "Queue all eligible signals through the pipeline's start node, replicating what ingest does on arrival. Use --new-only to skip signals that already have executions for any action node in the pipeline.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := jsonBody(map[string]any{"new_only": pipelineRunNewOnly})
		if err != nil {
			return fmt.Errorf("pipeline run: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/pipelines/"+url.PathEscape(args[0])+"/run", body)
		if err != nil {
			return fmt.Errorf("pipeline run: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("pipeline run: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Queued %s signals for pipeline #%s\n",
			formatField(result["queued"]), args[0])
		return nil
	},
}

func init() {
	pipelineAddCmd.Flags().StringVar(&pipelineDescription, "description", "", "Pipeline description")

	pipelineUpdateCmd.Flags().StringVar(&pipelineUpdateName, "name", "", "New name")
	pipelineUpdateCmd.Flags().StringVar(&pipelineUpdateDesc, "description", "", "New description")

	pipelineRunCmd.Flags().BoolVar(&pipelineRunNewOnly, "new-only", false, "Only dispatch signals without existing executions in this pipeline")

	pipelineCmd.AddCommand(pipelineListCmd)
	pipelineCmd.AddCommand(pipelineShowCmd)
	pipelineCmd.AddCommand(pipelineAddCmd)
	pipelineCmd.AddCommand(pipelineUpdateCmd)
	pipelineCmd.AddCommand(pipelineRemoveCmd)
	pipelineCmd.AddCommand(pipelineActivateCmd)
	pipelineCmd.AddCommand(pipelineDeactivateCmd)
	pipelineCmd.AddCommand(pipelineRunCmd)
	rootCmd.AddCommand(pipelineCmd)
}
