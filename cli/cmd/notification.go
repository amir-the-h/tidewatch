package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var notificationCmd = &cobra.Command{
	Use:   "notification",
	Short: "Manage Pushover notifications",
}

var (
	notifEventType  string
	notifActiveOnly bool
)

var notificationListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all notification configs",
	RunE: func(cmd *cobra.Command, args []string) error {
		u, err := url.Parse(baseURL + "/api/notifications")
		if err != nil {
			return fmt.Errorf("notification list: build URL: %w", err)
		}
		q := u.Query()
		if notifEventType != "" {
			q.Set("event_type", notifEventType)
		}
		if cmd.Flags().Changed("active") {
			q.Set("active", strconv.FormatBool(notifActiveOnly))
		}
		u.RawQuery = q.Encode()

		resp, err := http.Get(u.String())
		if err != nil {
			return fmt.Errorf("notification list: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var notifications []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&notifications); err != nil {
			return fmt.Errorf("notification list: decode response: %w", err)
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tEVENT_TYPE\tACTIVE\tPRIORITY")
		for _, n := range notifications {
			id := formatField(n["id"])
			name := formatField(n["name"])
			eventType := formatField(n["event_type"])
			active := formatField(n["active"])
			priority := formatField(n["priority"])
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, name, eventType, active, priority)
		}
		return w.Flush()
	},
}

var (
	notifName              string
	notifCreateEventType   string
	notifFilterRuleID      int64
	notifFilterModelName   string
	notifFilterModelResult string
	notifPriority          int
	notifSound             string
	notifDevice            string
	notifTitle             string
	notifURL               string
	notifURLTitle          string
	notifTTL               int
	notifHTML              bool
	notifRetry             int
	notifExpire            int
	notifMessageTemplate   string
)

var notificationCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new notification config",
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]interface{}{
			"name":             notifName,
			"event_type":       notifCreateEventType,
			"priority":         notifPriority,
			"sound":            notifSound,
			"device":           notifDevice,
			"title":            notifTitle,
			"url":              notifURL,
			"url_title":        notifURLTitle,
			"ttl":              notifTTL,
			"html":             notifHTML,
			"retry":            notifRetry,
			"expire":           notifExpire,
			"message_template": notifMessageTemplate,
		}

		if cmd.Flags().Changed("filter-rule-id") {
			body["filter_rule_id"] = notifFilterRuleID
		}
		if cmd.Flags().Changed("filter-model-name") {
			body["filter_model_name"] = notifFilterModelName
		}
		if notifFilterModelResult != "" {
			var modelResult interface{}
			if err := json.Unmarshal([]byte(notifFilterModelResult), &modelResult); err != nil {
				return fmt.Errorf("notification create: parse --filter-model-result: %w", err)
			}
			body["filter_model_result"] = modelResult
		}

		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("notification create: marshal body: %w", err)
		}

		resp, err := http.Post(baseURL+"/api/notifications", "application/json", bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("notification create: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var created map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("notification create: decode response: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Notification created: id=%s name=%s event_type=%s\n",
			formatField(created["id"]),
			formatField(created["name"]),
			formatField(created["event_type"]),
		)
		return nil
	},
}

var notificationGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a notification config by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		resp, err := http.Get(baseURL + "/api/notifications/" + id)
		if err != nil {
			return fmt.Errorf("notification get: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		var result interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("notification get: decode response: %w", err)
		}

		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("notification get: marshal output: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

var notificationDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a notification config by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodDelete, baseURL+"/api/notifications/"+id, nil)
		if err != nil {
			return fmt.Errorf("notification delete: build request: %w", err)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("notification delete: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Notification %s deleted\n", id)
		return nil
	},
}

var notificationActivateCmd = &cobra.Command{
	Use:   "activate <id>",
	Short: "Activate a notification config by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodPost, baseURL+"/api/notifications/"+id+"/activate", http.NoBody)
		if err != nil {
			return fmt.Errorf("notification activate: build request: %w", err)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("notification activate: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Notification %s activated\n", id)
		return nil
	},
}

var notificationDeactivateCmd = &cobra.Command{
	Use:   "deactivate <id>",
	Short: "Deactivate a notification config by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := url.PathEscape(args[0])
		req, err := http.NewRequest(http.MethodPost, baseURL+"/api/notifications/"+id+"/deactivate", http.NoBody)
		if err != nil {
			return fmt.Errorf("notification deactivate: build request: %w", err)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("notification deactivate: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Notification %s deactivated\n", id)
		return nil
	},
}

var notificationTestCmd = &cobra.Command{
	Use:   "test <id>",
	Short: "Send a test notification by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		notifID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("notification test: invalid id %q: %w", args[0], err)
		}

		body := map[string]int64{"notification_id": notifID}
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("notification test: marshal body: %w", err)
		}

		resp, err := http.Post(baseURL+"/api/notifications/test", "application/json", bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("notification test: %w", err)
		}
		defer resp.Body.Close()

		if err := handleAPIError(cmd, resp); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Test notification sent for id=%d\n", notifID)
		return nil
	},
}

func init() {
	// list flags
	notificationListCmd.Flags().StringVar(&notifEventType, "event-type", "", "Filter by event type")
	notificationListCmd.Flags().BoolVar(&notifActiveOnly, "active", false, "Filter by active status")

	// create flags
	notificationCreateCmd.Flags().StringVar(&notifName, "name", "", "Notification name")
	notificationCreateCmd.Flags().StringVar(&notifCreateEventType, "event-type", "", "Event type to trigger on")
	notificationCreateCmd.Flags().Int64Var(&notifFilterRuleID, "filter-rule-id", 0, "Filter by stream rule ID")
	notificationCreateCmd.Flags().StringVar(&notifFilterModelName, "filter-model-name", "", "Filter by model name")
	notificationCreateCmd.Flags().StringVar(&notifFilterModelResult, "filter-model-result", "", "Filter by model result (JSON string)")
	notificationCreateCmd.Flags().IntVar(&notifPriority, "priority", 0, "Pushover priority (-2 to 2)")
	notificationCreateCmd.Flags().StringVar(&notifSound, "sound", "", "Pushover sound")
	notificationCreateCmd.Flags().StringVar(&notifDevice, "device", "", "Target device")
	notificationCreateCmd.Flags().StringVar(&notifTitle, "title", "", "Notification title")
	notificationCreateCmd.Flags().StringVar(&notifURL, "url", "", "Supplementary URL")
	notificationCreateCmd.Flags().StringVar(&notifURLTitle, "url-title", "", "Supplementary URL title")
	notificationCreateCmd.Flags().IntVar(&notifTTL, "ttl", 0, "Message TTL in seconds")
	notificationCreateCmd.Flags().BoolVar(&notifHTML, "html", false, "Enable HTML formatting")
	notificationCreateCmd.Flags().IntVar(&notifRetry, "retry", 0, "Retry interval in seconds (emergency priority)")
	notificationCreateCmd.Flags().IntVar(&notifExpire, "expire", 0, "Expiration in seconds (emergency priority)")
	notificationCreateCmd.Flags().StringVar(&notifMessageTemplate, "message-template", "", "Go template for notification message")
	_ = notificationCreateCmd.MarkFlagRequired("name")
	_ = notificationCreateCmd.MarkFlagRequired("event-type")

	notificationCmd.AddCommand(notificationListCmd)
	notificationCmd.AddCommand(notificationCreateCmd)
	notificationCmd.AddCommand(notificationGetCmd)
	notificationCmd.AddCommand(notificationDeleteCmd)
	notificationCmd.AddCommand(notificationActivateCmd)
	notificationCmd.AddCommand(notificationDeactivateCmd)
	notificationCmd.AddCommand(notificationTestCmd)
	rootCmd.AddCommand(notificationCmd)
}
