package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var streamCmd = &cobra.Command{
	Use:   "stream",
	Short: "Manage the filtered stream connection",
}

var streamStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show stream connection status",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Get(baseURL + "/api/stream/status")
		if err != nil {
			return fmt.Errorf("stream status: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("stream status: decode: %w", err)
		}

		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("stream status: marshal: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	},
}

var streamConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to the X filtered stream",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Post(baseURL+"/api/stream/connect", "application/json", nil)
		if err != nil {
			return fmt.Errorf("stream connect: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Stream connected.")
		return nil
	},
}

var streamDisconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Disconnect from the X filtered stream",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Post(baseURL+"/api/stream/disconnect", "application/json", nil)
		if err != nil {
			return fmt.Errorf("stream disconnect: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Stream disconnected.")
		return nil
	},
}

func init() {
	streamCmd.AddCommand(streamStatusCmd)
	streamCmd.AddCommand(streamConnectCmd)
	streamCmd.AddCommand(streamDisconnectCmd)
	rootCmd.AddCommand(streamCmd)
}
