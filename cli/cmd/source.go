package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Manage sources (configured connector instances: X stream, RSS feed, …)",
	Long: `Manage sources. A source is a configured instance of a connector type
(see "tidewatch source types"). Active sources run in the server: stream
sources stay connected, poll sources are fetched on their interval.

Secret config fields come back masked as "••••"; sending the mask back on
update keeps the stored value.`,
}

// sourceDo sends a request, checks the status and decodes the JSON response
// into out (when out is non-nil).
func sourceDo(cmd *cobra.Command, op, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		var err error
		if r, err = jsonBody(body); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
	}
	resp, err := apiRequest(method, baseURL+path, r)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close()
	if err := handleAPIError(cmd, resp); err != nil {
		return err
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode response: %w", op, err)
	}
	return nil
}

func printJSON(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

var (
	sourceListType   string
	sourceListActive string
)

var sourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sources",
	RunE: func(cmd *cobra.Command, _ []string) error {
		q := url.Values{}
		if sourceListType != "" {
			q.Set("type", sourceListType)
		}
		if sourceListActive != "" {
			q.Set("active", sourceListActive)
		}
		path := "/api/sources"
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		var sources []map[string]any
		if err := sourceDo(cmd, "source list", http.MethodGet, path, nil, &sources); err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tTIER\tREGION\tACTIVE")
		for _, s := range sources {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				formatField(s["id"]), formatField(s["name"]), formatField(s["type"]),
				formatField(s["tier"]), formatField(s["region"]), formatField(s["active"]))
		}
		return w.Flush()
	},
}

var sourceShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a source (secret config values masked)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var src map[string]any
		if err := sourceDo(cmd, "source show", http.MethodGet, "/api/sources/"+url.PathEscape(args[0]), nil, &src); err != nil {
			return err
		}
		return printJSON(cmd, src)
	},
}

var sourceTypesCmd = &cobra.Command{
	Use:   "types",
	Short: "List connector types and their config fields",
	RunE: func(cmd *cobra.Command, _ []string) error {
		var types []map[string]any
		if err := sourceDo(cmd, "source types", http.MethodGet, "/api/sources/types", nil, &types); err != nil {
			return err
		}
		if len(types) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No connector types registered.")
			return nil
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TYPE\tLABEL\tMODE\tFIELDS")
		for _, t := range types {
			var keys []string
			if fields, ok := t["fields"].([]any); ok {
				for _, f := range fields {
					fm, _ := f.(map[string]any)
					k := formatField(fm["key"])
					if fm["required"] == true {
						k += "*"
					}
					keys = append(keys, k)
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", formatField(t["type"]), formatField(t["label"]),
				formatField(t["mode"]), strings.Join(keys, ", "))
		}
		return w.Flush()
	},
}

// Flags shared by create and update.
var (
	sourceFlagName      string
	sourceFlagType      string
	sourceFlagConfig    string
	sourceFlagSet       []string
	sourceFlagInterval  int
	sourceFlagTier      int
	sourceFlagRegion    string
	sourceFlagLanguage  string
	sourceFlagStateAff  bool
	sourceFlagActive    bool
	sourceFlagClearTier bool
)

func addSourceFlags(c *cobra.Command) {
	c.Flags().StringVar(&sourceFlagName, "name", "", "Unique source name")
	c.Flags().StringVar(&sourceFlagConfig, "config", "", "Connector config as a JSON object (replaces the stored config on update)")
	c.Flags().StringArrayVar(&sourceFlagSet, "set", nil, "Set one config key: key=value (value parsed as JSON when valid, else a string); repeatable")
	c.Flags().IntVar(&sourceFlagInterval, "interval", 0, "Poll interval in seconds (poll sources; default: the connector's)")
	c.Flags().IntVar(&sourceFlagTier, "tier", 0, "Reliability tier 1–4 (1 wire/official … 4 aggregator/blog)")
	c.Flags().StringVar(&sourceFlagRegion, "region", "", "Free-form region tag")
	c.Flags().StringVar(&sourceFlagLanguage, "language", "", "Language tag (e.g. en)")
	c.Flags().BoolVar(&sourceFlagStateAff, "state-affiliated", false, "Mark the source as state-affiliated")
	c.Flags().BoolVar(&sourceFlagActive, "active", false, "Activate the source (create) / set active (update)")
}

// sourceBody builds the create/update body from the flags that were set.
// base is the config to apply --set keys to when --config isn't given.
func sourceBody(cmd *cobra.Command, base map[string]any) (map[string]any, error) {
	body := map[string]any{}
	f := cmd.Flags()
	if f.Changed("name") {
		body["name"] = sourceFlagName
	}
	if f.Changed("type") {
		body["type"] = sourceFlagType
	}
	cfg := base
	if f.Changed("config") {
		cfg = map[string]any{}
		if err := json.Unmarshal([]byte(sourceFlagConfig), &cfg); err != nil {
			return nil, fmt.Errorf("--config must be a JSON object: %w", err)
		}
	}
	if len(sourceFlagSet) > 0 {
		if cfg == nil {
			cfg = map[string]any{}
		}
		for _, kv := range sourceFlagSet {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				return nil, fmt.Errorf("--set %q: want key=value", kv)
			}
			var parsed any
			if err := json.Unmarshal([]byte(v), &parsed); err != nil {
				parsed = v
			}
			cfg[k] = parsed
		}
	}
	if f.Changed("config") || len(sourceFlagSet) > 0 {
		body["config"] = cfg
	}
	if f.Changed("interval") {
		if sourceFlagInterval == 0 {
			body["clear_poll_interval"] = true
		} else {
			body["poll_interval_s"] = sourceFlagInterval
		}
	}
	if f.Changed("tier") {
		body["tier"] = sourceFlagTier
	}
	if sourceFlagClearTier {
		body["clear_tier"] = true
	}
	if f.Changed("region") {
		body["region"] = sourceFlagRegion
	}
	if f.Changed("language") {
		body["language"] = sourceFlagLanguage
	}
	if f.Changed("state-affiliated") {
		body["state_affiliated"] = sourceFlagStateAff
	}
	if f.Changed("active") {
		body["active"] = sourceFlagActive
	}
	return body, nil
}

var sourceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a source",
	Example: `  tidewatch source create --type x_stream --name "X stream" \
    --set bearer_token=$X_BEARER_TOKEN --set tier=pro --active
  tidewatch source create --type x_search --name "X search" \
    --set bearer_token=$X_BEARER_TOKEN --set 'rule_ids=[1,2]' --active
  tidewatch source create --type rss --name "BBC World" \
    --set url=https://feeds.bbci.co.uk/news/world/rss.xml --tier 2 --region global --language en --active`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := sourceBody(cmd, nil)
		if err != nil {
			return fmt.Errorf("source create: %w", err)
		}
		var src map[string]any
		if err := sourceDo(cmd, "source create", http.MethodPost, "/api/sources", body, &src); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Source created: id=%s name=%s type=%s active=%s\n",
			formatField(src["id"]), formatField(src["name"]), formatField(src["type"]), formatField(src["active"]))
		return nil
	},
}

var sourceUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a source (only the flags given change; --set edits single config keys)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "/api/sources/" + url.PathEscape(args[0])
		var base map[string]any
		if len(sourceFlagSet) > 0 && !cmd.Flags().Changed("config") {
			// --set edits the stored config; masked secrets are kept by the server.
			var cur struct {
				Config map[string]any `json:"config"`
			}
			if err := sourceDo(cmd, "source update", http.MethodGet, path, nil, &cur); err != nil {
				return err
			}
			base = cur.Config
		}
		body, err := sourceBody(cmd, base)
		if err != nil {
			return fmt.Errorf("source update: %w", err)
		}
		var src map[string]any
		if err := sourceDo(cmd, "source update", http.MethodPut, path, body, &src); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Source %s updated\n", formatField(src["id"]))
		return nil
	},
}

var sourceDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a source (its signals stay; their source links go)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := sourceDo(cmd, "source delete", http.MethodDelete, "/api/sources/"+url.PathEscape(args[0]), nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Source %s deleted\n", args[0])
		return nil
	},
}

func sourceActionCmd(use, short, action, done string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := sourceDo(cmd, "source "+use, http.MethodPost, "/api/sources/"+url.PathEscape(args[0])+"/"+action, nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Source %s %s\n", args[0], done)
			return nil
		},
	}
}

var sourceActivateCmd = sourceActionCmd("activate", "Activate a source (starts it)", "activate", "activated")
var sourceDeactivateCmd = sourceActionCmd("deactivate", "Deactivate a source (stops it)", "deactivate", "deactivated")

func printRunQueued(cmd *cobra.Command, run map[string]any) {
	fmt.Fprintf(cmd.OutOrStdout(), "Run %s queued (%s); check it with: tidewatch source run %s\n",
		formatField(run["id"]), formatField(run["kind"]), formatField(run["id"]))
}

var sourceFetchCmd = &cobra.Command{
	Use:   "fetch <id>",
	Short: "Poll a poll source once now (works on inactive sources too)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var run map[string]any
		if err := sourceDo(cmd, "source fetch", http.MethodPost, "/api/sources/"+url.PathEscape(args[0])+"/fetch", nil, &run); err != nil {
			return err
		}
		printRunQueued(cmd, run)
		return nil
	},
}

var sourceBackfillParams string

var sourceBackfillCmd = &cobra.Command{
	Use:   "backfill <id>",
	Short: "Run a historical fetch (connectors that support backfill)",
	Example: `  tidewatch source backfill 2 \
    --params '{"start_time":"2026-10-01T00:00:00Z","end_time":"2026-10-08T00:00:00Z","limit":500}'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]any{}
		if sourceBackfillParams != "" {
			if err := json.Unmarshal([]byte(sourceBackfillParams), &params); err != nil {
				return fmt.Errorf("source backfill: --params must be a JSON object: %w", err)
			}
		}
		var run map[string]any
		if err := sourceDo(cmd, "source backfill", http.MethodPost, "/api/sources/"+url.PathEscape(args[0])+"/backfill", params, &run); err != nil {
			return err
		}
		printRunQueued(cmd, run)
		return nil
	},
}

var sourceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show every source's runtime status",
	RunE: func(cmd *cobra.Command, _ []string) error {
		var list []map[string]any
		if err := sourceDo(cmd, "source status", http.MethodGet, "/api/sources/status", nil, &list); err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATE\tLAST SUCCESS\tNEXT RUN\tFETCHED\tNEW\tLAST ERROR")
		for _, s := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				formatField(s["source_id"]), formatField(s["name"]), formatField(s["type"]), formatField(s["state"]),
				formatField(s["last_success_at"]), formatField(s["next_run_at"]),
				formatField(s["fetched"]), formatField(s["new"]), formatField(s["last_error"]))
		}
		return w.Flush()
	},
}

var sourceRunsLimit int

var sourceRunsCmd = &cobra.Command{
	Use:   "runs <id>",
	Short: "List a source's fetch and backfill runs, newest first",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := fmt.Sprintf("/api/sources/%s/runs?limit=%d", url.PathEscape(args[0]), sourceRunsLimit)
		var runs []map[string]any
		if err := sourceDo(cmd, "source runs", http.MethodGet, path, nil, &runs); err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tKIND\tSTATUS\tFETCHED\tNEW\tCREATED\tERROR")
		for _, r := range runs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				formatField(r["id"]), formatField(r["kind"]), formatField(r["status"]),
				formatField(r["fetched_count"]), formatField(r["new_count"]), formatField(r["created_at"]), formatField(r["error"]))
		}
		return w.Flush()
	},
}

var sourceRunCmd = &cobra.Command{
	Use:   "run <run-id>",
	Short: "Show one source run",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var run map[string]any
		if err := sourceDo(cmd, "source run", http.MethodGet, "/api/source-runs/"+url.PathEscape(args[0]), nil, &run); err != nil {
			return err
		}
		return printJSON(cmd, run)
	},
}

func init() {
	sourceListCmd.Flags().StringVar(&sourceListType, "type", "", "Filter by connector type")
	sourceListCmd.Flags().StringVar(&sourceListActive, "active", "", "Filter by active (true|false)")

	addSourceFlags(sourceCreateCmd)
	sourceCreateCmd.Flags().StringVar(&sourceFlagType, "type", "", "Connector type (see: tidewatch source types)")
	_ = sourceCreateCmd.MarkFlagRequired("type")
	_ = sourceCreateCmd.MarkFlagRequired("name")
	addSourceFlags(sourceUpdateCmd)
	sourceUpdateCmd.Flags().BoolVar(&sourceFlagClearTier, "clear-tier", false, "Remove the tier (unrated)")

	sourceBackfillCmd.Flags().StringVar(&sourceBackfillParams, "params", "", "Backfill params as a JSON object (connector-defined)")
	sourceRunsCmd.Flags().IntVar(&sourceRunsLimit, "limit", 20, "Maximum number of runs")

	sourceCmd.AddCommand(sourceListCmd, sourceShowCmd, sourceTypesCmd, sourceCreateCmd, sourceUpdateCmd,
		sourceDeleteCmd, sourceActivateCmd, sourceDeactivateCmd, sourceFetchCmd, sourceBackfillCmd,
		sourceStatusCmd, sourceRunsCmd, sourceRunCmd)
	rootCmd.AddCommand(sourceCmd)
}
