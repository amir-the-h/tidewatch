package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users",
}

var (
	userPassword string
	userRole     string
)

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all users",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiGet(baseURL + "/api/users")
		if err != nil {
			return fmt.Errorf("user list: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var users []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
			return fmt.Errorf("user list: decode: %w", err)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tUSERNAME\tROLE\tCREATED_AT")
		for _, u := range users {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				formatField(u["id"]),
				formatField(u["username"]),
				formatField(u["role"]),
				formatField(u["created_at"]),
			)
		}
		return w.Flush()
	},
}

var userAddCmd = &cobra.Command{
	Use:   "add <username>",
	Short: "Create a new user",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := jsonBody(map[string]string{
			"username": args[0],
			"password": userPassword,
			"role":     userRole,
		})
		if err != nil {
			return fmt.Errorf("user add: %w", err)
		}
		resp, err := apiPost(baseURL+"/api/users", body)
		if err != nil {
			return fmt.Errorf("user add: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		var created map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("user add: decode: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "User created: id=%s username=%s role=%s\n",
			formatField(created["id"]),
			formatField(created["username"]),
			formatField(created["role"]),
		)
		return nil
	},
}

var (
	userUpdateUsername string
	userUpdatePassword string
	userUpdateRole     string
)

var userUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a user's username, password, or role",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]string{}
		if cmd.Flags().Changed("username") {
			body["username"] = userUpdateUsername
		}
		if cmd.Flags().Changed("password") {
			body["password"] = userUpdatePassword
		}
		if cmd.Flags().Changed("role") {
			body["role"] = userUpdateRole
		}
		r, err := jsonBody(body)
		if err != nil {
			return fmt.Errorf("user update: %w", err)
		}
		resp, err := apiPut(baseURL+"/api/users/"+url.PathEscape(args[0]), r)
		if err != nil {
			return fmt.Errorf("user update: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "User %s updated\n", args[0])
		return nil
	},
}

var userRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Delete a user",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := apiDelete(baseURL + "/api/users/" + url.PathEscape(args[0]))
		if err != nil {
			return fmt.Errorf("user remove: %w", err)
		}
		defer resp.Body.Close()
		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "User %s removed\n", args[0])
		return nil
	},
}

func init() {
	userAddCmd.Flags().StringVar(&userPassword, "password", "", "Password")
	userAddCmd.Flags().StringVar(&userRole, "role", "viewer", "Role (admin or viewer)")
	_ = userAddCmd.MarkFlagRequired("password")

	userUpdateCmd.Flags().StringVar(&userUpdateUsername, "username", "", "New username")
	userUpdateCmd.Flags().StringVar(&userUpdatePassword, "password", "", "New password")
	userUpdateCmd.Flags().StringVar(&userUpdateRole, "role", "", "New role (admin or viewer)")

	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userAddCmd)
	userCmd.AddCommand(userUpdateCmd)
	userCmd.AddCommand(userRemoveCmd)
	rootCmd.AddCommand(userCmd)
}
