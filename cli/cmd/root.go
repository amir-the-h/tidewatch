package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var baseURL string

var rootCmd = &cobra.Command{
	Use:   "iran-watcher",
	Short: "X (Twitter) Filtered Stream monitor for Iran-related news",
	Long:  "iran-watcher monitors X (Twitter) filtered streams for Iran-related news, stores tweets, and categorizes them via local ONNX models.",
}

func init() {
	defaultBaseURL := "http://localhost:8080"
	if v := os.Getenv("IRAN_WATCHER_BASE_URL"); v != "" {
		defaultBaseURL = v
	}
	rootCmd.PersistentFlags().StringVar(&baseURL, "base-url", defaultBaseURL, "Base URL of the iran-watcher REST API (env: IRAN_WATCHER_BASE_URL)")
}

// tokenFile returns the path to the stored JWT token.
func tokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".iran-watcher-token"
	}
	return filepath.Join(home, ".iran-watcher", "token")
}

// loadToken reads the stored JWT token, returning empty string if not found.
func loadToken() string {
	data, err := os.ReadFile(tokenFile())
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(data))
}

// saveToken writes the JWT token to disk.
func saveToken(token string) error {
	path := tokenFile()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0600)
}

// apiRequest performs an authenticated HTTP request, attaching the stored Bearer token.
func apiRequest(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := loadToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

func apiGet(url string) (*http.Response, error) {
	return apiRequest(http.MethodGet, url, nil)
}

func apiPost(url string, body io.Reader) (*http.Response, error) {
	return apiRequest(http.MethodPost, url, body)
}

func apiPut(url string, body io.Reader) (*http.Response, error) {
	return apiRequest(http.MethodPut, url, body)
}

func apiDelete(url string) (*http.Response, error) {
	return apiRequest(http.MethodDelete, url, nil)
}

// jsonBody marshals v into a reader suitable for request bodies.
func jsonBody(v any) (io.Reader, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// Execute runs the root command with os.Stdout and os.Stderr.
func Execute() error {
	return rootCmd.Execute()
}

// ExecuteWithArgs runs the root command with the given writer for output and args.
// This is used in tests to capture output.
func ExecuteWithArgs(out io.Writer, errOut io.Writer, args ...string) error {
	// Reset all flag values to their defaults to prevent test order-dependence.
	resetFlags(rootCmd)

	rootCmd.SetOut(out)
	rootCmd.SetErr(errOut)
	rootCmd.SetArgs(args)

	// Propagate writers to subcommands.
	for _, sub := range rootCmd.Commands() {
		sub.SetOut(out)
		sub.SetErr(errOut)
		for _, subsub := range sub.Commands() {
			subsub.SetOut(out)
			subsub.SetErr(errOut)
		}
	}

	err := rootCmd.Execute()
	// Reset args so subsequent calls don't reuse them.
	rootCmd.SetArgs(nil)
	return err
}

// resetFlags resets all flag values in a command tree to their defaults.
func resetFlags(c *cobra.Command) {
	c.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	c.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// apiError represents the error response from the API.
type apiError struct {
	Error string `json:"error"`
}

// handleAPIError checks the response status code and returns an error with the API error message if non-200.
// Cobra prints the returned error to stderr automatically; do not also print it here.
func handleAPIError(_ *cobra.Command, resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var apiErr apiError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		return fmt.Errorf("API error: status %d", resp.StatusCode)
	}
	return fmt.Errorf("API error: %s", apiErr.Error)
}

// exitIfError prints the error and exits with code 1.
func exitIfError(err error) {
	if err != nil {
		os.Exit(1)
	}
}
