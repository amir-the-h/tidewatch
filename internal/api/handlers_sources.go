package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/source"
	"github.com/amir-the-h/tidewatch/internal/store"
)

// WithSources wires the source manager (and through it the connector
// registry) used by the /api/sources routes.
func (s *Server) WithSources(m *source.Manager) *Server {
	s.sources = m
	return s
}

// sourceRequest is the body of POST /api/sources and PUT /api/sources/{id}.
// On PUT every field is optional and only the ones sent change; config is
// replaced as a whole, with masked secrets ("••••") keeping their stored value.
type sourceRequest struct {
	Name            *string         `json:"name"`
	Type            *string         `json:"type"`
	Config          json.RawMessage `json:"config"`
	PollIntervalS   *int            `json:"poll_interval_s"`
	ClearInterval   bool            `json:"clear_poll_interval"` // PUT: back to the connector default
	Tier            *int            `json:"tier"`
	ClearTier       bool            `json:"clear_tier"` // PUT: unrated
	StateAffiliated *bool           `json:"state_affiliated"`
	Region          *string         `json:"region"`
	Language        *string         `json:"language"`
	Active          *bool           `json:"active"`
}

// requireSources writes 503 and returns false when no source manager is wired.
func (s *Server) requireSources(w http.ResponseWriter) bool {
	if s.sources == nil {
		writeError(w, http.StatusServiceUnavailable, "source manager not available")
		return false
	}
	return true
}

// secretKeys returns the secret config keys of a connector type.
func (s *Server) secretKeys(typ string) []string {
	if s.sources == nil {
		return nil
	}
	conn, ok := s.sources.Registry().Get(typ)
	if !ok {
		return nil
	}
	return source.SecretKeys(conn.Descriptor().Fields)
}

// maskSource returns a copy of src with its secret config values masked.
func (s *Server) maskSource(src *model.Source) *model.Source {
	cp := *src
	cp.Config = source.MaskConfig(s.secretKeys(src.Type), src.Config)
	return &cp
}

func (s *Server) maskSources(list []*model.Source) []*model.Source {
	out := make([]*model.Source, len(list))
	for i, src := range list {
		out[i] = s.maskSource(src)
	}
	return out
}

func (s *Server) broadcastSources(r *http.Request) {
	if list, err := s.store.ListSources(r.Context(), store.SourceFilter{}); err == nil {
		s.broadcastEvent("sources", s.maskSources(list))
	}
}

// writeSourceError maps manager and validation errors to HTTP statuses.
func (s *Server) writeSourceError(w http.ResponseWriter, op string, err error) {
	var verr *source.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(w, http.StatusBadRequest, verr.Error())
	case errors.Is(err, source.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "source not found")
	case strings.Contains(err.Error(), "UNIQUE constraint failed: sources.name"):
		writeError(w, http.StatusConflict, "a source with this name already exists")
	default:
		s.logger.Error("api: "+op, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to "+op)
	}
}

func (s *Server) loadSource(w http.ResponseWriter, r *http.Request) (*model.Source, bool) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source ID")
		return nil, false
	}
	src, err := s.store.GetSource(r.Context(), id)
	if err != nil {
		s.writeSourceError(w, "get source", err)
		return nil, false
	}
	return src, true
}

// checkInstances enforces the connector's MaxInstances for an active src.
func (s *Server) checkInstances(r *http.Request, src *model.Source) error {
	all, err := s.store.ListSources(r.Context(), store.SourceFilter{Type: src.Type})
	if err != nil {
		return err
	}
	return s.sources.Registry().CheckInstances(src, all)
}

func (s *Server) reloadSource(r *http.Request, id int64) {
	if err := s.sources.Reload(r.Context(), id); err != nil {
		s.logger.Warn("api: reload source", "source_id", id, "error", err)
	}
}

// GET /api/sources?type=&active=
func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	filter := store.SourceFilter{Type: r.URL.Query().Get("type")}
	if a := r.URL.Query().Get("active"); a != "" {
		active, err := strconv.ParseBool(a)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid active parameter")
			return
		}
		filter.Active = &active
	}
	list, err := s.store.ListSources(r.Context(), filter)
	if err != nil {
		s.writeSourceError(w, "list sources", err)
		return
	}
	writeJSON(w, http.StatusOK, s.maskSources(list))
}

// GET /api/sources/{id}
func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	src, ok := s.loadSource(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.maskSource(src))
}

// POST /api/sources
func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireSources(w) {
		return
	}
	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	src := &model.Source{Config: req.Config}
	if req.Type == nil || *req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	src.Type = *req.Type
	applySourceRequest(src, &req)

	if err := s.sources.Registry().Validate(src); err != nil {
		s.writeSourceError(w, "create source", err)
		return
	}
	if err := s.checkInstances(r, src); err != nil {
		s.writeSourceError(w, "create source", err)
		return
	}
	if err := s.store.SaveSource(r.Context(), src); err != nil {
		s.writeSourceError(w, "create source", err)
		return
	}
	s.reloadSource(r, src.ID)
	s.broadcastSources(r)
	writeJSON(w, http.StatusCreated, s.maskSource(src))
}

// PUT /api/sources/{id}
func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireSources(w) {
		return
	}
	src, ok := s.loadSource(w, r)
	if !ok {
		return
	}
	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Type != nil && *req.Type != src.Type {
		writeError(w, http.StatusBadRequest, "a source's type can't be changed; create a new source")
		return
	}
	if len(req.Config) > 0 {
		merged, err := source.MergeSecrets(s.secretKeys(src.Type), req.Config, src.Config)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		src.Config = merged
	}
	applySourceRequest(src, &req)

	if err := s.sources.Registry().Validate(src); err != nil {
		s.writeSourceError(w, "update source", err)
		return
	}
	if err := s.checkInstances(r, src); err != nil {
		s.writeSourceError(w, "update source", err)
		return
	}
	if err := s.store.UpdateSource(r.Context(), src); err != nil {
		s.writeSourceError(w, "update source", err)
		return
	}
	s.reloadSource(r, src.ID)
	s.broadcastSources(r)
	writeJSON(w, http.StatusOK, s.maskSource(src))
}

// applySourceRequest copies the fields req sets onto src (all but type and config).
func applySourceRequest(src *model.Source, req *sourceRequest) {
	if req.Name != nil {
		src.Name = strings.TrimSpace(*req.Name)
	}
	if req.ClearInterval {
		src.PollIntervalS = nil
	} else if req.PollIntervalS != nil {
		src.PollIntervalS = req.PollIntervalS
	}
	if req.ClearTier {
		src.Tier = nil
	} else if req.Tier != nil {
		src.Tier = req.Tier
	}
	if req.StateAffiliated != nil {
		src.StateAffiliated = *req.StateAffiliated
	}
	if req.Region != nil {
		src.Region = *req.Region
	}
	if req.Language != nil {
		src.Language = *req.Language
	}
	if req.Active != nil {
		src.Active = *req.Active
	}
}

// DELETE /api/sources/{id}
func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireSources(w) {
		return
	}
	src, ok := s.loadSource(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSource(r.Context(), src.ID); err != nil {
		s.writeSourceError(w, "delete source", err)
		return
	}
	s.reloadSource(r, src.ID)
	s.broadcastSources(r)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/sources — body: optional ids, active, type; empty = all.
func (s *Server) handleBulkDeleteSources(w http.ResponseWriter, r *http.Request) {
	if !s.requireSources(w) {
		return
	}
	var f store.BulkDeleteSourcesFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteSources(r.Context(), f)
	if err != nil {
		s.writeSourceError(w, "bulk delete sources", err)
		return
	}
	if err := s.sources.ReloadAll(r.Context()); err != nil {
		s.logger.Warn("api: reload sources", "error", err)
	}
	s.broadcastSources(r)
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

// POST /api/sources/{id}/activate
func (s *Server) handleActivateSource(w http.ResponseWriter, r *http.Request) {
	s.setSourceActive(w, r, true)
}

// POST /api/sources/{id}/deactivate
func (s *Server) handleDeactivateSource(w http.ResponseWriter, r *http.Request) {
	s.setSourceActive(w, r, false)
}

func (s *Server) setSourceActive(w http.ResponseWriter, r *http.Request, active bool) {
	if !s.requireSources(w) {
		return
	}
	src, ok := s.loadSource(w, r)
	if !ok {
		return
	}
	src.Active = active
	if active {
		if _, known := s.sources.Registry().Get(src.Type); !known {
			writeError(w, http.StatusBadRequest, "connector type \""+src.Type+"\" is not available")
			return
		}
		if err := s.checkInstances(r, src); err != nil {
			s.writeSourceError(w, "activate source", err)
			return
		}
	}
	if err := s.store.SetSourceActive(r.Context(), src.ID, active); err != nil {
		s.writeSourceError(w, "set source active", err)
		return
	}
	s.reloadSource(r, src.ID)
	s.broadcastSources(r)
	writeJSON(w, http.StatusOK, s.maskSource(src))
}

// GET /api/sources/types
func (s *Server) handleListSourceTypes(w http.ResponseWriter, _ *http.Request) {
	if !s.requireSources(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.sources.Registry().Descriptors())
}

// GET /api/sources/status
func (s *Server) handleListSourceStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireSources(w) {
		return
	}
	list, err := s.sources.Statuses(r.Context())
	if err != nil {
		s.writeSourceError(w, "list source status", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/sources/{id}/fetch — poll now; 202 with the pending run.
func (s *Server) handleFetchSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireSources(w) {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source ID")
		return
	}
	run, err := s.sources.Fetch(r.Context(), id)
	if err != nil {
		s.writeSourceError(w, "fetch source", err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

// POST /api/sources/{id}/backfill — body: connector-defined params; 202 with the pending run.
func (s *Server) handleBackfillSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireSources(w) {
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source ID")
		return
	}
	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	run, err := s.sources.Backfill(r.Context(), id, params)
	if err != nil {
		s.writeSourceError(w, "backfill source", err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

// GET /api/sources/{id}/runs?limit=
func (s *Server) handleListSourceRuns(w http.ResponseWriter, r *http.Request) {
	src, ok := s.loadSource(w, r)
	if !ok {
		return
	}
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit parameter")
			return
		}
		limit = n
	}
	runs, err := s.store.ListSourceRuns(r.Context(), src.ID, limit)
	if err != nil {
		s.writeSourceError(w, "list source runs", err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

// GET /api/source-runs/{id}
func (s *Server) handleGetSourceRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetSourceRun(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "source run not found")
		return
	}
	if err != nil {
		s.writeSourceError(w, "get source run", err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}
