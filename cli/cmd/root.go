package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

var baseURL string

var rootCmd = &cobra.Command{
	Use:   "iran-watcher",
	Short: "X (Twitter) Filtered Stream monitor for Iran-related news",
	Long:  "iran-watcher monitors X (Twitter) filtered streams for Iran-related news, stores tweets, and categorizes them via local ONNX models.",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&baseURL, "base-url", "http://localhost:8080", "Base URL of the iran-watcher REST API")
}

// Execute runs the root command with os.Stdout and os.Stderr.
func Execute() error {
	return rootCmd.Execute()
}

// ExecuteWithArgs runs the root command with the given writer for output and args.
// This is used in tests to capture output.
func ExecuteWithArgs(out io.Writer, errOut io.Writer, args ...string) error {
	// Reset the command state for test reuse
	rootCmd.ResetFlags()
	rootCmd.PersistentFlags().StringVar(&baseURL, "base-url", "http://localhost:8080", "Base URL of the iran-watcher REST API")

	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	rootCmd.SetArgs(args)

	// Propagate writers to subcommands
	for _, sub := range rootCmd.Commands() {
		sub.SetOut(out)
		sub.SetErr(errOut)
		for _, subsub := range sub.Commands() {
			subsub.SetOut(out)
			subsub.SetErr(errOut)
		}
	}

	err := rootCmd.Execute()
	// Reset args so subsequent calls don't reuse them
	rootCmd.SetArgs(nil)
	return err
}

// apiError represents the error response from the API.
type apiError struct {
	Error string `json:"error"`
}

// handleAPIError checks the response status code and returns an error with the API error message if non-200.
func handleAPIError(cmd *cobra.Command, resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var apiErr apiError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		return fmt.Errorf("API error: status %d", resp.StatusCode)
	}
	msg := fmt.Sprintf("API error: %s", apiErr.Error)
	fmt.Fprintln(cmd.ErrOrStderr(), msg)
	return fmt.Errorf("%s", msg)
}

// exitIfError prints the error and exits with code 1.
func exitIfError(err error) {
	if err != nil {
		os.Exit(1)
	}
}
