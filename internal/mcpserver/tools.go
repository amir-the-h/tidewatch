package mcpserver

// ToolDef declares one REST endpoint as an MCP tool.
type ToolDef struct {
	Name        string
	Description string
	Method      string     // GET, POST, PUT, DELETE
	Path        string     // e.g. "/api/signals/{id}"
	PathParams  []ParamDef // always required; substituted into path template
	QueryParams []ParamDef // optional query string fields
	Body        *BodyDef   // nil if endpoint takes no request body
}

// ParamDef describes a single parameter.
type ParamDef struct {
	Name        string
	Description string
	Required    bool
	Type        string // "string" | "integer" | "boolean"
}

// BodyDef describes a request body.
type BodyDef struct {
	Description string
}

// AllTools returns the complete ToolDef table for all tidewatch REST endpoints.
// Source of truth: internal/api/server.go route registrations.
func AllTools() []ToolDef {
	return []ToolDef{

		// ── Auth ─────────────────────────────────────────────────────────────
		{
			Name:        "auth_me",
			Description: "Return the currently authenticated user's profile.",
			Method:      "GET",
			Path:        "/api/auth/me",
		},

		// ── Health ───────────────────────────────────────────────────────────
		{
			Name:        "health_get",
			Description: "Return API health status.",
			Method:      "GET",
			Path:        "/api/health",
		},

		// ── X rules ──────────────────────────────────────────────────────────
		{
			Name:        "x_rules_list",
			Description: "List all X Filtered Stream rules.",
			Method:      "GET",
			Path:        "/api/x/rules",
		},
		{
			Name:        "x_rules_get",
			Description: "Get a single X rule by ID.",
			Method:      "GET",
			Path:        "/api/x/rules/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
		},
		{
			Name:        "x_rules_create",
			Description: "Create an X Filtered Stream rule.",
			Method:      "POST",
			Path:        "/api/x/rules",
			Body: &BodyDef{
				Description: "Rule definition. Fields: name (string, required), expression (object, required; {src: termID}, {srcs: [ids]}, {op: \"is:retweet\"}, {AND|OR: [...]}, {NOT: {...}}, {rule: ruleID}), local (bool), active (bool).",
			},
		},
		{
			Name:        "x_rules_update",
			Description: "Update an X rule.",
			Method:      "PUT",
			Path:        "/api/x/rules/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
			Body: &BodyDef{
				Description: "Updated rule fields: name (string), expression (object), local (bool).",
			},
		},
		{
			Name:        "x_rules_delete",
			Description: "Delete an X rule by ID.",
			Method:      "DELETE",
			Path:        "/api/x/rules/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
		},
		{
			Name:        "x_rules_bulk_delete",
			Description: "Bulk-delete X rules. All filters optional — omit all to delete every X rule. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/x/rules",
			Body: &BodyDef{
				Description: "ids ([]int, optional), active (bool, optional), local (bool, optional). All optional.",
			},
		},
		{
			Name:        "x_rules_activate",
			Description: "Activate an X rule. A non-local rule is pushed to X through the active x_stream source; with none active it is synced when the stream next starts.",
			Method:      "POST",
			Path:        "/api/x/rules/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
		},
		{
			Name:        "x_rules_deactivate",
			Description: "Deactivate an X rule.",
			Method:      "POST",
			Path:        "/api/x/rules/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Rule ID", Required: true}},
		},

		// ── X terms (rule atoms) ─────────────────────────────────────────────
		{
			Name:        "x_terms_list",
			Description: "List all X terms: the rule atoms (keywords, hashtags, accounts…) X rules combine.",
			Method:      "GET",
			Path:        "/api/x/terms",
		},
		{
			Name:        "x_terms_create",
			Description: "Create an X term.",
			Method:      "POST",
			Path:        "/api/x/terms",
			Body: &BodyDef{
				Description: "Term definition. Fields: type (string, required: keyword, phrase, hashtag, cashtag, mention, account, to_account, retweets_of, url, url_contains, context, entity, conversation_id, place, place_country, point_radius, bounding_box, lang, is, has, followers_count, tweets_count, following_count, listed_count, sample), value (string, required), name (string, defaults to value).",
			},
		},
		{
			Name:        "x_terms_update",
			Description: "Update an X term.",
			Method:      "PUT",
			Path:        "/api/x/terms/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Term ID", Required: true}},
			Body: &BodyDef{
				Description: "Updated term fields: name (string), type (string), value (string).",
			},
		},
		{
			Name:        "x_terms_delete",
			Description: "Delete an X term (refused while a rule references it).",
			Method:      "DELETE",
			Path:        "/api/x/terms/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Term ID", Required: true}},
		},
		{
			Name:        "x_terms_bulk_delete",
			Description: "Bulk-delete X terms. All filters optional — omit all to delete every term. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/x/terms",
			Body: &BodyDef{
				Description: "ids ([]int, optional), active (bool, optional), type (string, optional). All optional.",
			},
		},
		{
			Name:        "x_terms_deactivate",
			Description: "Deactivate an X term (refused while an active rule references it).",
			Method:      "POST",
			Path:        "/api/x/terms/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Term ID", Required: true}},
		},

		// ── Sources (configured connector instances) ─────────────────────────
		{
			Name:        "sources_list",
			Description: "List sources: configured connector instances (X stream, X search, RSS feed, …). Secret config values come back as \"••••\".",
			Method:      "GET",
			Path:        "/api/sources",
			QueryParams: []ParamDef{
				{Name: "type", Type: "string", Description: "Filter by connector type"},
				{Name: "active", Type: "boolean", Description: "Filter by active state"},
			},
		},
		{
			Name:        "sources_get",
			Description: "Get a source by ID (secret config values masked).",
			Method:      "GET",
			Path:        "/api/sources/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
		},
		{
			Name:        "sources_create",
			Description: "Create a source. Use source_types_list for the connector types and their config fields.",
			Method:      "POST",
			Path:        "/api/sources",
			Body: &BodyDef{
				Description: "name (string, required, unique), type (string, required: a connector type), config (object: the connector's fields), poll_interval_s (int, poll connectors only; omit for the default), tier (int 1–4: 1 wire/official … 4 aggregator/blog), state_affiliated (bool), region (string), language (string), active (bool, default false).",
			},
		},
		{
			Name:        "sources_update",
			Description: "Update a source. Only the fields sent change; config replaces the stored config, and a secret sent back as \"••••\" keeps its stored value. The type can't change.",
			Method:      "PUT",
			Path:        "/api/sources/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
			Body: &BodyDef{
				Description: "Any of: name, config, poll_interval_s, clear_poll_interval (bool), tier, clear_tier (bool), state_affiliated, region, language, active.",
			},
		},
		{
			Name:        "sources_delete",
			Description: "Delete a source. Its signals stay; their links to it are removed.",
			Method:      "DELETE",
			Path:        "/api/sources/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
		},
		{
			Name:        "sources_bulk_delete",
			Description: "Bulk-delete sources. All filters optional — omit all to delete every source. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/sources",
			Body: &BodyDef{
				Description: "ids ([]int, optional), active (bool, optional), type (string, optional). All optional.",
			},
		},
		{
			Name:        "sources_activate",
			Description: "Activate a source: it starts running (stream connects, poll source is fetched on its interval).",
			Method:      "POST",
			Path:        "/api/sources/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
		},
		{
			Name:        "sources_deactivate",
			Description: "Deactivate a source: it stops running.",
			Method:      "POST",
			Path:        "/api/sources/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
		},
		{
			Name:        "sources_fetch",
			Description: "Poll a poll source once now (active or not). Returns 202 with a pending source run; check it with source_runs_get.",
			Method:      "POST",
			Path:        "/api/sources/{id}/fetch",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
		},
		{
			Name:        "sources_backfill",
			Description: "Run a historical fetch for a source whose connector supports backfill. Returns 202 with a pending source run.",
			Method:      "POST",
			Path:        "/api/sources/{id}/backfill",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
			Body: &BodyDef{
				Description: "Connector-defined backfill params (object). x_search: start_time and end_time (RFC 3339), limit (tweets per query).",
			},
		},
		{
			Name:        "sources_status_get",
			Description: "Runtime status of every source: state (running, idle, backoff, error, stopped), last run and success, last error, next run, signals fetched and new since the server started.",
			Method:      "GET",
			Path:        "/api/sources/status",
		},
		{
			Name:        "source_types_list",
			Description: "List connector types: mode (stream or poll), dedup namespace, signal kinds, config fields (key, type, required, secret), max instances, poll intervals, backfill support.",
			Method:      "GET",
			Path:        "/api/sources/types",
		},
		{
			Name:        "source_runs_list",
			Description: "List a source's fetch and backfill runs, newest first.",
			Method:      "GET",
			Path:        "/api/sources/{id}/runs",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Source ID", Required: true}},
			QueryParams: []ParamDef{{Name: "limit", Type: "integer", Description: "Maximum runs (default 50)"}},
		},
		{
			Name:        "source_runs_get",
			Description: "Get one source run (status pending, running, done or error; fetched and new counts).",
			Method:      "GET",
			Path:        "/api/source-runs/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "string", Description: "Run ID (UUID)", Required: true}},
		},

		// ── Signals ──────────────────────────────────────────────────────────
		{
			Name:        "signals_list",
			Description: "List signals (X posts today; news articles once RSS sources exist) with optional filtering. Returns paginated results with total count.",
			Method:      "GET",
			Path:        "/api/signals",
			QueryParams: []ParamDef{
				{Name: "rule_id", Type: "integer", Description: "Filter by X rule ID"},
				{Name: "source_id", Type: "integer", Description: "Filter by the source that saw the signal"},
				{Name: "source_type", Type: "string", Description: "Filter by connector type of a source that saw the signal (e.g. rss)"},
				{Name: "kind", Type: "string", Description: "Filter by signal kind: post or article"},
				{Name: "from", Type: "string", Description: "Start of the published_at window (RFC3339)"},
				{Name: "to", Type: "string", Description: "End of the published_at window (RFC3339)"},
				{Name: "page", Type: "integer", Description: "Page number (default 1)"},
				{Name: "per_page", Type: "integer", Description: "Results per page (default 20)"},
			},
		},
		{
			Name:        "signals_get",
			Description: "Get a single signal by ID including its action execution history and metadata.",
			Method:      "GET",
			Path:        "/api/signals/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Signal ID (internal integer ID, not the X tweet ID)", Required: true}},
		},
		{
			Name:        "signals_delete",
			Description: "Delete signals by their IDs, with their executions, metadata and entity tracks.",
			Method:      "DELETE",
			Path:        "/api/signals",
			Body: &BodyDef{
				Description: "Object with field: ids (array of integer signal IDs, required).",
			},
		},
		{
			Name:        "signals_reprocess",
			Description: "Re-queue signals through the new_signal pipelines. Optionally targets specific action IDs.",
			Method:      "POST",
			Path:        "/api/signals/reprocess",
			Body: &BodyDef{
				Description: "Object with fields: signal_ids (array of integers, required), action_ids (array of integers, optional — targets specific actions).",
			},
		},
		{
			Name:        "signals_noise_delete",
			Description: "Delete X signals that match no rule, or only local rules.",
			Method:      "DELETE",
			Path:        "/api/signals/noise",
		},

		// ── Map ──────────────────────────────────────────────────────────────
		{
			Name:        "map_pins_list",
			Description: "List geo-event map pins sourced from action_execution_metadata, with signal_published_at for date-heat coloring.",
			Method:      "GET",
			Path:        "/api/map",
		},

		// ── Tracked Entities ─────────────────────────────────────────────────
		{
			Name:        "tracked_entities_list",
			Description: "List tracked entities. Optionally filter by type or active status.",
			Method:      "GET",
			Path:        "/api/tracked-entities",
			QueryParams: []ParamDef{
				{Name: "type", Type: "string", Description: "Filter by entity type (e.g. person, organization, location)"},
				{Name: "active", Type: "boolean", Description: "Filter by active status"},
			},
		},
		{
			Name:        "tracked_entities_get",
			Description: "Get a tracked entity by ID.",
			Method:      "GET",
			Path:        "/api/tracked-entities/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "tracked_entities_create",
			Description: "Create a new tracked entity.",
			Method:      "POST",
			Path:        "/api/tracked-entities",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), type (string), aliases (array of strings), notes (string).",
			},
		},
		{
			Name:        "tracked_entities_update",
			Description: "Update a tracked entity.",
			Method:      "PUT",
			Path:        "/api/tracked-entities/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), type (string), aliases (array of strings), notes (string).",
			},
		},
		{
			Name:        "tracked_entities_delete",
			Description: "Delete a tracked entity by ID.",
			Method:      "DELETE",
			Path:        "/api/tracked-entities/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "tracked_entities_bulk_delete",
			Description: "Bulk-delete tracked entities. All filters optional — omit all to delete every tracked entity. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/tracked-entities",
			Body: &BodyDef{
				Description: "ids ([]int, optional), active (bool, optional), type (string, optional). All optional.",
			},
		},
		{
			Name:        "tracked_entities_activate",
			Description: "Activate a tracked entity.",
			Method:      "POST",
			Path:        "/api/tracked-entities/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "tracked_entities_deactivate",
			Description: "Deactivate a tracked entity.",
			Method:      "POST",
			Path:        "/api/tracked-entities/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "tracked_entities_tracks_list",
			Description: "List NER track occurrences for a tracked entity (paginated).",
			Method:      "GET",
			Path:        "/api/tracked-entities/{id}/tracks",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
			QueryParams: []ParamDef{
				{Name: "from", Type: "string", Description: "Start time (RFC3339)"},
				{Name: "to", Type: "string", Description: "End time (RFC3339)"},
				{Name: "page", Type: "integer", Description: "Page number"},
				{Name: "per_page", Type: "integer", Description: "Results per page"},
			},
		},
		{
			Name:        "tracked_entities_alias_suggestions",
			Description: "List NER-derived alias suggestions for an existing tracked entity.",
			Method:      "GET",
			Path:        "/api/tracked-entities/{id}/alias-suggestions",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Entity ID", Required: true}},
		},
		{
			Name:        "alias_suggestions_search",
			Description: "Search NER alias suggestions by name and optional entity type (for new entity creation).",
			Method:      "GET",
			Path:        "/api/alias-suggestions",
			QueryParams: []ParamDef{
				{Name: "name", Type: "string", Description: "Name to search for"},
				{Name: "type", Type: "string", Description: "Entity type filter"},
			},
		},

		// ── Entity Candidates ────────────────────────────────────────────────
		{
			Name:        "entity_candidates_list",
			Description: "List auto-discovered entity candidates pending review.",
			Method:      "GET",
			Path:        "/api/entity-candidates",
		},
		{
			Name:        "entity_candidates_get",
			Description: "Get an entity candidate by ID.",
			Method:      "GET",
			Path:        "/api/entity-candidates/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
		},
		{
			Name:        "entity_candidates_promote",
			Description: "Promote an entity candidate to a full tracked entity.",
			Method:      "POST",
			Path:        "/api/entity-candidates/{id}/promote",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
		},
		{
			Name:        "entity_candidates_reject",
			Description: "Reject an entity candidate.",
			Method:      "POST",
			Path:        "/api/entity-candidates/{id}/reject",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
		},
		{
			Name:        "entity_candidates_merge",
			Description: "Merge an entity candidate as an alias of an existing tracked entity.",
			Method:      "POST",
			Path:        "/api/entity-candidates/{id}/merge",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with field: entity_id (integer, the tracked entity to merge into).",
			},
		},
		{
			Name:        "entity_candidates_delete",
			Description: "Delete an entity candidate.",
			Method:      "DELETE",
			Path:        "/api/entity-candidates/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Candidate ID", Required: true}},
		},

		{
			Name:        "entity_candidates_bulk_delete",
			Description: "Bulk-delete entity candidates. All filters optional — omit all to delete every candidate. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/entity-candidates",
			Body: &BodyDef{
				Description: "ids ([]int, optional), status (string: pending/promoted/rejected, optional), type (string: I-PER/I-LOC/I-ORG/I-MISC, optional), auto_promoted (bool, optional), min_confidence (float, optional), max_confidence (float, optional), min_occurrences (int, optional), max_occurrences (int, optional). All optional.",
			},
		},

		// ── Entity Connections ───────────────────────────────────────────────
		{
			Name:        "entity_connections_list",
			Description: "List all entity co-occurrence connections with joined entity names.",
			Method:      "GET",
			Path:        "/api/entity-connections",
		},
		{
			Name:        "entity_connections_delete",
			Description: "Truncate all entity connections (admin only).",
			Method:      "DELETE",
			Path:        "/api/entity-connections",
		},
		{
			Name:        "entity_connections_subgraph",
			Description: "Get the BFS subgraph of entity connections centered on an entity.",
			Method:      "GET",
			Path:        "/api/entity-connections/subgraph",
			QueryParams: []ParamDef{
				{Name: "entity_id", Type: "integer", Description: "Center entity ID", Required: true},
				{Name: "depth", Type: "integer", Description: "BFS depth (default 2, max 5)"},
				{Name: "min_weight", Type: "integer", Description: "Minimum co-occurrence weight filter"},
			},
		},

		// ── Pipelines (DAG workflows) ────────────────────────────────────────
		{
			Name:        "pipelines_list",
			Description: "List all DAG pipeline workflows.",
			Method:      "GET",
			Path:        "/api/pipelines",
		},
		{
			Name:        "pipelines_get",
			Description: "Get a pipeline by ID.",
			Method:      "GET",
			Path:        "/api/pipelines/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_create",
			Description: "Create a new DAG pipeline workflow.",
			Method:      "POST",
			Path:        "/api/pipelines",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), description (string).",
			},
		},
		{
			Name:        "pipelines_update",
			Description: "Update a pipeline's name or description.",
			Method:      "PUT",
			Path:        "/api/pipelines/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), description (string).",
			},
		},
		{
			Name:        "pipelines_delete",
			Description: "Delete a pipeline.",
			Method:      "DELETE",
			Path:        "/api/pipelines/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_bulk_delete",
			Description: "Bulk-delete pipelines. All filters optional — omit all to delete every pipeline. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/pipelines",
			Body: &BodyDef{
				Description: "ids ([]int, optional), active (bool, optional). All optional.",
			},
		},
		{
			Name:        "pipelines_activate",
			Description: "Activate a pipeline so it participates in signal processing.",
			Method:      "POST",
			Path:        "/api/pipelines/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_deactivate",
			Description: "Deactivate a pipeline.",
			Method:      "POST",
			Path:        "/api/pipelines/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_graph_get",
			Description: "Get the DAG graph (nodes + edges) for a pipeline.",
			Method:      "GET",
			Path:        "/api/pipelines/{id}/graph",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
		},
		{
			Name:        "pipelines_graph_save",
			Description: "Replace the full DAG graph for a pipeline. Detects and rejects cycles.",
			Method:      "PUT",
			Path:        "/api/pipelines/{id}/graph",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
			Body: &BodyDef{
				Description: `Object with fields: nodes (array of PipelineNodeRequest), edges (array of PipelineEdgeRequest).

IMPORTANT — node identity uses client_id strings, NOT database IDs:
  Node fields:
    client_id      string   (required) — caller-chosen stable identifier, e.g. "start", "cond", "ext"; used by edges to reference this node
    type           string   — "start" | "action" | "condition" | "pipeline"
    label          string
    pos_x, pos_y   float
    action_id      int      (action nodes only)
    sub_pipeline_id int     (pipeline nodes only)
    conditions     array    (condition nodes only) — [{key, op, value}]

  Edge fields:
    source_client_id  string  — client_id of the source node
    source_handle     string  — "default" | "pass" | "fail"
    target_client_id  string  — client_id of the target node

Do NOT use database node IDs (id field) in edges — the server always assigns new IDs on save. Always reference nodes via client_id / source_client_id / target_client_id.`,
			},
		},
		{
			Name:        "pipelines_run",
			Description: "Dispatch historical signals through a pipeline. Returns queued count (async: true means running in background).",
			Method:      "POST",
			Path:        "/api/pipelines/{id}/run",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with optional fields: new_only (bool, skip signals already processed by the start node's action), start_node_id (int, dispatch from this action or pipeline node instead of the pipeline's Start node — full context from prior runs is available automatically), order (string, \"asc\" for oldest-first dispatch or \"desc\" for newest-first; default \"desc\").",
			},
		},

		// ── Triggers ─────────────────────────────────────────────────────────
		{
			Name:        "triggers_list",
			Description: "List all available trigger types (new_signal, new_categorization, entity_matched, etc.).",
			Method:      "GET",
			Path:        "/api/triggers",
		},
		{
			Name:        "triggers_links_list",
			Description: "List all trigger-to-pipeline links.",
			Method:      "GET",
			Path:        "/api/triggers/links",
		},
		{
			Name:        "triggers_links_create",
			Description: "Create a trigger-to-pipeline link (connects a trigger type to a pipeline entry node).",
			Method:      "POST",
			Path:        "/api/triggers/links",
			Body: &BodyDef{
				Description: "Object with fields: trigger_type (string, required), pipeline_id (integer, required).",
			},
		},
		{
			Name:        "triggers_links_delete",
			Description: "Delete a trigger-to-pipeline link.",
			Method:      "DELETE",
			Path:        "/api/triggers/links/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Link ID", Required: true}},
		},
		{
			Name:        "triggers_links_bulk_delete",
			Description: "Bulk-delete trigger-to-pipeline links. All filters optional — omit all to delete every link. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/triggers/links",
			Body: &BodyDef{
				Description: "ids ([]int, optional), pipeline_id (int, optional), trigger_type (string, optional). All optional.",
			},
		},
		{
			Name:        "triggers_links_activate",
			Description: "Activate a trigger-to-pipeline link.",
			Method:      "POST",
			Path:        "/api/triggers/links/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Link ID", Required: true}},
		},
		{
			Name:        "triggers_links_deactivate",
			Description: "Deactivate a trigger-to-pipeline link.",
			Method:      "POST",
			Path:        "/api/triggers/links/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Link ID", Required: true}},
		},

		// ── Actions ──────────────────────────────────────────────────────────
		{
			Name:        "actions_list",
			Description: "List actions (execution units). Optionally filter by active status or type.",
			Method:      "GET",
			Path:        "/api/actions",
			QueryParams: []ParamDef{
				{Name: "active", Type: "boolean", Description: "Filter by active status"},
				{Name: "type", Type: "string", Description: "Filter by type: onnx, ollama, internal, notification"},
			},
		},
		{
			Name:        "actions_get",
			Description: "Get an action by ID.",
			Method:      "GET",
			Path:        "/api/actions/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "actions_create",
			Description: "Create a new action. type must be: onnx, ollama, internal, or notification.",
			Method:      "POST",
			Path:        "/api/actions",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), type (string, required: onnx|ollama|internal|notification), trigger_type (string), config (object, pipeline-specific JSON).",
			},
		},
		{
			Name:        "actions_update",
			Description: "Update an action's name, type, or config.",
			Method:      "PUT",
			Path:        "/api/actions/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), type (string), config (object).",
			},
		},
		{
			Name:        "actions_delete",
			Description: "Delete an action.",
			Method:      "DELETE",
			Path:        "/api/actions/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "actions_bulk_delete",
			Description: "Bulk-delete actions. All filters optional — omit all to delete every action. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/actions",
			Body: &BodyDef{
				Description: "ids ([]int, optional), active (bool, optional), type (string: onnx/ollama/internal, optional). All optional.",
			},
		},
		{
			Name:        "actions_activate",
			Description: "Activate an action so it processes incoming triggers.",
			Method:      "POST",
			Path:        "/api/actions/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "actions_deactivate",
			Description: "Deactivate an action.",
			Method:      "POST",
			Path:        "/api/actions/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "actions_handlers_list",
			Description: "List registered internal action handler names (entity_matcher, twitter_retweet, etc.).",
			Method:      "GET",
			Path:        "/api/actions/handlers",
		},
		{
			Name:        "models_list",
			Description: "List available ONNX model files in the models directory.",
			Method:      "GET",
			Path:        "/api/models",
		},

		// ── Action Executions ────────────────────────────────────────────────
		{
			Name:        "action_executions_list",
			Description: "List execution history for a specific action (paginated).",
			Method:      "GET",
			Path:        "/api/actions/{id}/executions",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
			QueryParams: []ParamDef{
				{Name: "page", Type: "integer", Description: "Page number"},
				{Name: "per_page", Type: "integer", Description: "Results per page"},
				{Name: "from", Type: "string", Description: "Start time (RFC3339)"},
				{Name: "to", Type: "string", Description: "End time (RFC3339)"},
			},
		},
		{
			Name:        "executions_list",
			Description: "List all action executions across all actions (paginated).",
			Method:      "GET",
			Path:        "/api/executions",
			QueryParams: []ParamDef{
				{Name: "signal_id", Type: "integer", Description: "Filter by signal ID"},
				{Name: "status", Type: "string", Description: "Filter by status (success, error)"},
				{Name: "page", Type: "integer", Description: "Page number"},
				{Name: "per_page", Type: "integer", Description: "Results per page"},
				{Name: "from", Type: "string", Description: "Start time (RFC3339)"},
				{Name: "to", Type: "string", Description: "End time (RFC3339)"},
			},
		},
		{
			Name:        "execution_metadata_get",
			Description: "Get the key-value metadata map for a single action execution.",
			Method:      "GET",
			Path:        "/api/executions/{id}/metadata",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Execution ID", Required: true}},
		},
		{
			Name:        "executions_delete",
			Description: "Bulk-delete execution records and their metadata. All filters are optional: omit all to delete every execution. Supply action_ids to scope to specific actions, pipeline_id to scope to all actions in a pipeline, signal_ids to scope to specific signals, or any combination.",
			Method:      "DELETE",
			Path:        "/api/executions",
			Body: &BodyDef{
				Description: "Object with optional fields: signal_ids (array of integers), action_ids (array of integers), pipeline_id (integer). If all are omitted, all executions are deleted.",
			},
		},

		// ── Action Backends ──────────────────────────────────────────────────
		{
			Name:        "action_backends_list",
			Description: "List Ollama backend priority assignments for an action.",
			Method:      "GET",
			Path:        "/api/actions/{id}/backends",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
		},
		{
			Name:        "action_backends_set",
			Description: "Set (replace) Ollama backend priority assignments for an action.",
			Method:      "PUT",
			Path:        "/api/actions/{id}/backends",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Action ID", Required: true}},
			Body: &BodyDef{
				Description: `Object with key "backends" containing an array: {"backends": [{"backend_id": integer, "priority": integer}, ...]}.  Replaces all existing assignments.`,
			},
		},
		{
			Name:        "action_backends_remove",
			Description: "Remove a specific Ollama backend assignment from an action.",
			Method:      "DELETE",
			Path:        "/api/actions/{id}/backends/{backend_id}",
			PathParams: []ParamDef{
				{Name: "id", Type: "integer", Description: "Action ID", Required: true},
				{Name: "backend_id", Type: "integer", Description: "Backend ID", Required: true},
			},
		},

		// ── Ollama Backends ──────────────────────────────────────────────────
		{
			Name:        "ollama_backends_list",
			Description: "List Ollama backends merged with live health and scoring stats.",
			Method:      "GET",
			Path:        "/api/ollama/backends",
		},
		{
			Name:        "ollama_backends_create",
			Description: "Register a new Ollama backend.",
			Method:      "POST",
			Path:        "/api/ollama/backends",
			Body: &BodyDef{
				Description: "Object with fields: name (string), url (string, required, e.g. http://localhost:11434).",
			},
		},
		{
			Name:        "ollama_backends_update",
			Description: "Update an Ollama backend's name or URL.",
			Method:      "PUT",
			Path:        "/api/ollama/backends/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Backend ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), url (string).",
			},
		},
		{
			Name:        "ollama_backends_delete",
			Description: "Delete an Ollama backend.",
			Method:      "DELETE",
			Path:        "/api/ollama/backends/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Backend ID", Required: true}},
		},
		{
			Name:        "ollama_backends_bulk_delete",
			Description: "Bulk-delete Ollama backends. All filters optional — omit all to delete every backend. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/ollama/backends",
			Body: &BodyDef{
				Description: "ids ([]int, optional), enabled (bool, optional). All optional.",
			},
		},
		{
			Name:        "ollama_backends_enable",
			Description: "Enable an Ollama backend (makes it eligible for task assignment).",
			Method:      "POST",
			Path:        "/api/ollama/backends/{id}/enable",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Backend ID", Required: true}},
		},
		{
			Name:        "ollama_backends_disable",
			Description: "Disable an Ollama backend.",
			Method:      "POST",
			Path:        "/api/ollama/backends/{id}/disable",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Backend ID", Required: true}},
		},

		// ── Queue ────────────────────────────────────────────────────────────
		{
			Name:        "queue_get",
			Description: "Get global queue status: pending task count and per-worker status (id, status, action, pipeline, signal_id, depth, duration, backend).",
			Method:      "GET",
			Path:        "/api/queue",
		},
		{
			Name:        "queue_flush",
			Description: "Flush all pending task queues, removing all queued tasks.",
			Method:      "DELETE",
			Path:        "/api/queue",
		},
		{
			Name:        "workers_buffer_flush",
			Description: "Flush the in-memory worker buffer for all pipelines.",
			Method:      "DELETE",
			Path:        "/api/workers/buffer",
		},
		{
			Name:        "workers_buffer_flush_pipeline",
			Description: "Flush the in-memory worker buffer for a specific pipeline name.",
			Method:      "DELETE",
			Path:        "/api/workers/buffer/{pipeline}",
			PathParams:  []ParamDef{{Name: "pipeline", Type: "string", Description: "Pipeline name", Required: true}},
		},

		// ── Summaries ────────────────────────────────────────────────────────
		{
			Name:        "summaries_list",
			Description: "List generated summaries.",
			Method:      "GET",
			Path:        "/api/summaries",
		},
		{
			Name:        "summaries_run",
			Description: "Trigger summary generation.",
			Method:      "POST",
			Path:        "/api/summaries/run",
		},

		// ── Digest Jobs ──────────────────────────────────────────────────────
		{
			Name:        "digest_list",
			Description: "List LLM digest jobs.",
			Method:      "GET",
			Path:        "/api/digest",
		},
		{
			Name:        "digest_create",
			Description: "Create and dispatch a new LLM digest job over a signal window. Returns 202.",
			Method:      "POST",
			Path:        "/api/digest",
			Body: &BodyDef{
				Description: "Optional object with fields: from (RFC3339), to (RFC3339), pipeline_id (integer).",
			},
		},
		{
			Name:        "digest_get",
			Description: "Get a digest job by ID. Poll until status is done or error.",
			Method:      "GET",
			Path:        "/api/digest/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Digest job ID", Required: true}},
		},
		{
			Name:        "digest_delete",
			Description: "Delete a digest job.",
			Method:      "DELETE",
			Path:        "/api/digest/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Digest job ID", Required: true}},
		},

		{
			Name:        "digest_bulk_delete",
			Description: "Bulk-delete digest jobs. All filters optional — omit all to delete every digest job. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/digest",
			Body: &BodyDef{
				Description: "ids ([]string, optional), status (string: pending/done/error, optional). All optional.",
			},
		},

		// ── Chat ─────────────────────────────────────────────────────────────
		{
			Name:        "chat_stream",
			Description: "Stream an agentic LLM response via SSE. The model autonomously calls tidewatch tools to answer questions.",
			Method:      "POST",
			Path:        "/api/chat/stream",
			Body: &BodyDef{
				Description: "Object with fields: action_id (string, optional — ID of the chat action to use), messages (array of {role, content}, required).",
			},
		},

		// ── Failures (dead-letter queue) ─────────────────────────────────────
		{
			Name:        "failures_list",
			Description: "List tasks in the dead-letter queue.",
			Method:      "GET",
			Path:        "/api/failures",
		},
		{
			Name:        "failures_clear",
			Description: "Clear all tasks from the dead-letter queue.",
			Method:      "DELETE",
			Path:        "/api/failures",
		},

		// ── Notification Providers ───────────────────────────────────────────
		{
			Name:        "notification_providers_list",
			Description: "List notification providers (Pushover, Telegram).",
			Method:      "GET",
			Path:        "/api/notification-providers",
		},
		{
			Name:        "notification_providers_create",
			Description: "Create a notification provider.",
			Method:      "POST",
			Path:        "/api/notification-providers",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), type (string: pushover|telegram), config (object: pushover needs {app_token, user_key}; telegram needs {bot_token}).",
			},
		},
		{
			Name:        "notification_providers_get",
			Description: "Get a notification provider by ID.",
			Method:      "GET",
			Path:        "/api/notification-providers/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
		},
		{
			Name:        "notification_providers_update",
			Description: "Update a notification provider.",
			Method:      "PUT",
			Path:        "/api/notification-providers/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), config (object).",
			},
		},
		{
			Name:        "notification_providers_delete",
			Description: "Delete a notification provider.",
			Method:      "DELETE",
			Path:        "/api/notification-providers/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
		},
		{
			Name:        "notification_providers_bulk_delete",
			Description: "Bulk-delete notification providers. All filters optional — omit all to delete every provider. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/notification-providers",
			Body: &BodyDef{
				Description: "ids ([]int, optional), active (bool, optional), type (string: pushover/telegram, optional). All optional.",
			},
		},
		{
			Name:        "notification_providers_activate",
			Description: "Activate a notification provider.",
			Method:      "POST",
			Path:        "/api/notification-providers/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
		},
		{
			Name:        "notification_providers_deactivate",
			Description: "Deactivate a notification provider.",
			Method:      "POST",
			Path:        "/api/notification-providers/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Provider ID", Required: true}},
		},

		// ── Notification Channels ────────────────────────────────────────────
		{
			Name:        "notification_channels_list",
			Description: "List notification channels.",
			Method:      "GET",
			Path:        "/api/notification-channels",
		},
		{
			Name:        "notification_channels_create",
			Description: "Create a notification channel.",
			Method:      "POST",
			Path:        "/api/notification-channels",
			Body: &BodyDef{
				Description: "Object with fields: name (string, required), provider_id (integer, required), message_template (string, supports ${key} substitution), config (object, e.g. {chat_id: string} for Telegram).",
			},
		},
		{
			Name:        "notification_channels_get",
			Description: "Get a notification channel by ID.",
			Method:      "GET",
			Path:        "/api/notification-channels/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
		},
		{
			Name:        "notification_channels_update",
			Description: "Update a notification channel.",
			Method:      "PUT",
			Path:        "/api/notification-channels/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: name (string), provider_id (integer), message_template (string), config (object).",
			},
		},
		{
			Name:        "notification_channels_delete",
			Description: "Delete a notification channel.",
			Method:      "DELETE",
			Path:        "/api/notification-channels/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
		},
		{
			Name:        "notification_channels_bulk_delete",
			Description: "Bulk-delete notification channels. All filters optional — omit all to delete every channel. Filters combine with AND.",
			Method:      "DELETE",
			Path:        "/api/notification-channels",
			Body: &BodyDef{
				Description: "ids ([]int, optional), active (bool, optional), provider_id (int, optional). All optional.",
			},
		},
		{
			Name:        "notification_channels_activate",
			Description: "Activate a notification channel.",
			Method:      "POST",
			Path:        "/api/notification-channels/{id}/activate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
		},
		{
			Name:        "notification_channels_deactivate",
			Description: "Deactivate a notification channel.",
			Method:      "POST",
			Path:        "/api/notification-channels/{id}/deactivate",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Channel ID", Required: true}},
		},

		// ── Users ────────────────────────────────────────────────────────────
		{
			Name:        "users_list",
			Description: "List all users (admin only).",
			Method:      "GET",
			Path:        "/api/users",
		},
		{
			Name:        "users_create",
			Description: "Create a new user (admin only).",
			Method:      "POST",
			Path:        "/api/users",
			Body: &BodyDef{
				Description: "Object with fields: username (string, required), password (string, required), role (string: admin|viewer).",
			},
		},
		{
			Name:        "users_update",
			Description: "Update a user's username, password, or role (admin only).",
			Method:      "PUT",
			Path:        "/api/users/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "User ID", Required: true}},
			Body: &BodyDef{
				Description: "Object with fields: username (string), password (string), role (string).",
			},
		},
		{
			Name:        "users_delete",
			Description: "Delete a user (admin only).",
			Method:      "DELETE",
			Path:        "/api/users/{id}",
			PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "User ID", Required: true}},
		},

		// ── Twitter Auth ─────────────────────────────────────────────────────
		{
			Name:        "twitter_auth_start",
			Description: "Get the OAuth 2.0 PKCE authorization URL to connect a Twitter account.",
			Method:      "GET",
			Path:        "/api/twitter/auth/start",
		},
		{
			Name:        "twitter_auth_status",
			Description: "Get the current Twitter OAuth connection status.",
			Method:      "GET",
			Path:        "/api/twitter/auth/status",
		},
		{
			Name:        "twitter_auth_delete",
			Description: "Disconnect the linked Twitter account.",
			Method:      "DELETE",
			Path:        "/api/twitter/auth",
		},
	}
}
