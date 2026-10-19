package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage API authentication",
}

var (
	loginUsername string
	loginPassword string
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in and save a JWT token to ~/.tidewatch/token",
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := jsonBody(map[string]string{
			"username": loginUsername,
			"password": loginPassword,
		})
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/auth/login", body)
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var result struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("login: decode: %w", err)
		}
		if err := saveToken(result.Token); err != nil {
			return fmt.Errorf("login: save token: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Logged in as %s — token saved to %s\n", loginUsername, tokenFile())
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove the stored JWT token",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.Remove(tokenFile()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("logout: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Logged out")
		return nil
	},
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the currently authenticated user",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/auth/me")
		if err != nil {
			return fmt.Errorf("whoami: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var user map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
			return fmt.Errorf("whoami: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "username: %s\nrole:     %s\n",
			formatField(user["username"]),
			formatField(user["role"]),
		)
		return nil
	},
}

func init() {
	loginCmd.Flags().StringVar(&loginUsername, "username", "", "Username")
	loginCmd.Flags().StringVar(&loginPassword, "password", "", "Password")
	_ = loginCmd.MarkFlagRequired("username")
	_ = loginCmd.MarkFlagRequired("password")

	authCmd.AddCommand(loginCmd)
	authCmd.AddCommand(logoutCmd)
	authCmd.AddCommand(whoamiCmd)
	rootCmd.AddCommand(authCmd)
}
