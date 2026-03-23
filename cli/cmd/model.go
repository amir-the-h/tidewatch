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

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Manage ONNX models",
}

var modelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all models",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Get(baseURL + "/api/models")
		if err != nil {
			return fmt.Errorf("model list: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var models []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
			return fmt.Errorf("model list: decode response: %w", err)
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tVERSION\tTYPE\tACTIVE")
		for _, m := range models {
			id := formatField(m["id"])
			name := formatField(m["name"])
			version := formatField(m["version"])
			typ := formatField(m["type"])
			active := formatField(m["active"])
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, name, version, typ, active)
		}
		return w.Flush()
	},
}

var (
	modelName    string
	modelVersion string
	modelPath    string
	modelType    string
)

var modelAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Register a new model",
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]string{
			"name":    modelName,
			"version": modelVersion,
			"path":    modelPath,
			"type":    modelType,
		}
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("model add: marshal body: %w", err)
		}

		resp, err := http.Post(baseURL+"/api/models", "application/json", bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("model add: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var created map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("model add: decode response: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Model added: id=%s name=%s version=%s\n",
			formatField(created["id"]),
			formatField(created["name"]),
			formatField(created["version"]),
		)
		return nil
	},
}

var modelRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a model by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodDelete, baseURL+"/api/models/"+id, nil)
		if err != nil {
			return fmt.Errorf("model remove: build request: %w", err)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("model remove: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Model %s removed\n", id)
		return nil
	},
}

var modelActivateCmd = &cobra.Command{
	Use:   "activate <id>",
	Short: "Activate a model by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodPost, baseURL+"/api/models/"+id+"/activate", http.NoBody)
		if err != nil {
			return fmt.Errorf("model activate: build request: %w", err)
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("model activate: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Model %s activated\n", id)
		return nil
	},
}

var modelDeactivateCmd = &cobra.Command{
	Use:   "deactivate <id>",
	Short: "Deactivate a model by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodPost, baseURL+"/api/models/"+id+"/deactivate", http.NoBody)
		if err != nil {
			return fmt.Errorf("model deactivate: build request: %w", err)
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("model deactivate: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Model %s deactivated\n", id)
		return nil
	},
}

func init() {
	modelAddCmd.Flags().StringVar(&modelName, "name", "", "Model name")
	modelAddCmd.Flags().StringVar(&modelVersion, "version", "", "Model version")
	modelAddCmd.Flags().StringVar(&modelPath, "path", "", "Path to ONNX model file")
	modelAddCmd.Flags().StringVar(&modelType, "type", "", "Model type")
	_ = modelAddCmd.MarkFlagRequired("name")
	_ = modelAddCmd.MarkFlagRequired("path")
	_ = modelAddCmd.MarkFlagRequired("type")

	modelCmd.AddCommand(modelListCmd)
	modelCmd.AddCommand(modelAddCmd)
	modelCmd.AddCommand(modelRemoveCmd)
	modelCmd.AddCommand(modelActivateCmd)
	modelCmd.AddCommand(modelDeactivateCmd)
	rootCmd.AddCommand(modelCmd)
}
