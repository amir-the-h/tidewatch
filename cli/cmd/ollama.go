package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var ollamaCmd = &cobra.Command{
	Use:   "ollama",
	Short: "Manage Ollama backends and pipelines",
}

// ---------------------------------------------------------------------------
// ollama backends
// ---------------------------------------------------------------------------

var ollamaBackendsCmd = &cobra.Command{
	Use:   "backends",
	Short: "Manage Ollama backends",
}

var ollamaBackendsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all Ollama backends with live stats",
	RunE:  runOllamaBackendsList,
}

var (
	addBackendName string
	addBackendURL  string
)

var ollamaBackendsAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new Ollama backend",
	RunE:  runOllamaBackendsAdd,
}

var ollamaBackendsUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an Ollama backend's name or URL",
	Args:  cobra.ExactArgs(1),
	RunE:  runOllamaBackendsUpdate,
}

var ollamaBackendsRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Delete an Ollama backend",
	Args:  cobra.ExactArgs(1),
	RunE:  runOllamaBackendsRemove,
}

var ollamaBackendsEnableCmd = &cobra.Command{
	Use:   "enable <id>",
	Short: "Enable an Ollama backend",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runOllamaBackendsSetEnabled(cmd, args, true)
	},
}

var ollamaBackendsDisableCmd = &cobra.Command{
	Use:   "disable <id>",
	Short: "Disable an Ollama backend",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runOllamaBackendsSetEnabled(cmd, args, false)
	},
}

func init() {
	ollamaBackendsAddCmd.Flags().StringVar(&addBackendName, "name", "", "display name (defaults to URL)")
	ollamaBackendsAddCmd.Flags().StringVar(&addBackendURL, "url", "", "Ollama server URL (required)")
	_ = ollamaBackendsAddCmd.MarkFlagRequired("url")

	ollamaBackendsUpdateCmd.Flags().StringVar(&addBackendName, "name", "", "new display name")
	ollamaBackendsUpdateCmd.Flags().StringVar(&addBackendURL, "url", "", "new URL")

	ollamaBackendsCmd.AddCommand(
		ollamaBackendsListCmd,
		ollamaBackendsAddCmd,
		ollamaBackendsUpdateCmd,
		ollamaBackendsRemoveCmd,
		ollamaBackendsEnableCmd,
		ollamaBackendsDisableCmd,
	)
	ollamaCmd.AddCommand(ollamaBackendsCmd)
	rootCmd.AddCommand(ollamaCmd)
}

func runOllamaBackendsList(cmd *cobra.Command, _ []string) error {
	resp, err := apiGet(baseURL + "/api/ollama/backends")
	if err != nil {
		return fmt.Errorf("ollama backends list: %w", err)
	}
	defer resp.Body.Close()
	if err := handleAPIError(cmd, resp); err != nil {
		return err
	}

	var backends []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&backends); err != nil {
		return fmt.Errorf("ollama backends list: decode: %w", err)
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tURL\tENABLED\tHEALTHY\tIN_USE\tSCORE\tREQUESTS\tLAST_ACTIVE")
	for _, b := range backends {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			formatField(b["id"]),
			formatField(b["name"]),
			formatField(b["url"]),
			formatField(b["enabled"]),
			formatField(b["healthy"]),
			formatField(b["in_use"]),
			formatField(b["score"]),
			formatField(b["request_count"]),
			formatField(b["last_active_at"]),
		)
	}
	return w.Flush()
}

func runOllamaBackendsAdd(cmd *cobra.Command, _ []string) error {
	body, err := jsonBody(map[string]any{
		"name": addBackendName,
		"url":  addBackendURL,
	})
	if err != nil {
		return err
	}
	resp, err := apiPost(baseURL+"/api/ollama/backends", body)
	if err != nil {
		return fmt.Errorf("ollama backends add: %w", err)
	}
	defer resp.Body.Close()
	if err := handleAPIError(cmd, resp); err != nil {
		return err
	}
	var b map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		return fmt.Errorf("ollama backends add: decode: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created backend %s (id=%s)\n", formatField(b["name"]), formatField(b["id"]))
	return nil
}

func runOllamaBackendsUpdate(cmd *cobra.Command, args []string) error {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid id: %s", args[0])
	}
	patch := map[string]any{}
	if addBackendName != "" {
		patch["name"] = addBackendName
	}
	if addBackendURL != "" {
		patch["url"] = addBackendURL
	}
	body, err := jsonBody(patch)
	if err != nil {
		return err
	}
	resp, err := apiPut(fmt.Sprintf("%s/api/ollama/backends/%d", baseURL, id), body)
	if err != nil {
		return fmt.Errorf("ollama backends update: %w", err)
	}
	defer resp.Body.Close()
	if err := handleAPIError(cmd, resp); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Updated backend %d\n", id)
	return nil
}

func runOllamaBackendsRemove(cmd *cobra.Command, args []string) error {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid id: %s", args[0])
	}
	resp, err := apiDelete(fmt.Sprintf("%s/api/ollama/backends/%d", baseURL, id))
	if err != nil {
		return fmt.Errorf("ollama backends remove: %w", err)
	}
	defer resp.Body.Close()
	if err := handleAPIError(cmd, resp); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted backend %d\n", id)
	return nil
}

func runOllamaBackendsSetEnabled(cmd *cobra.Command, args []string, enabled bool) error {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid id: %s", args[0])
	}
	action := "enable"
	if !enabled {
		action = "disable"
	}
	resp, err := apiPost(fmt.Sprintf("%s/api/ollama/backends/%d/%s", baseURL, id, action), nil)
	if err != nil {
		return fmt.Errorf("ollama backends %s: %w", action, err)
	}
	defer resp.Body.Close()
	if err := handleAPIError(cmd, resp); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Backend %d %sd\n", id, action)
	return nil
}
