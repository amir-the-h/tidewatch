package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/action"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
	filter := store.ActionFilter{}

	if v := r.URL.Query().Get("type"); v != "" {
		t := model.ActionType(v)
		filter.Type = &t
	}
	if v := r.URL.Query().Get("active"); v != "" {
		b := v == "true"
		filter.Active = &b
	}

	actions, err := s.store.ListActions(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list actions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list actions")
		return
	}
	writeJSON(w, http.StatusOK, actions)
}

func (s *Server) handleCreateAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string          `json:"name"`
		Type    model.ActionType `json:"type"`
		RuleIDs []int64         `json:"rule_ids"`
		Config  json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	switch req.Type {
	case model.ActionTypePipeline, model.ActionTypeRetweet, model.ActionTypeLike, model.ActionTypeQuote:
	default:
		writeError(w, http.StatusBadRequest, "invalid action type")
		return
	}

	if req.RuleIDs == nil {
		req.RuleIDs = []int64{}
	}
	if len(req.Config) == 0 {
		req.Config = json.RawMessage("{}")
	}

	a := &model.Action{
		Name:      req.Name,
		Type:      req.Type,
		Active:    true,
		RuleIDs:   req.RuleIDs,
		Config:    req.Config,
		CreatedAt: time.Now(),
	}
	if err := s.store.SaveAction(r.Context(), a); err != nil {
		s.logger.Error("api: create action", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create action")
		return
	}

	if err := s.reloadActionRegistry(r.Context()); err != nil {
		s.logger.Error("api: create action: reload registry", "error", err)
	}

	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleUpdateAction(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action ID")
		return
	}

	existing, err := s.store.GetAction(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}

	var req struct {
		Name    string          `json:"name"`
		RuleIDs []int64         `json:"rule_ids"`
		Config  json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.RuleIDs != nil {
		existing.RuleIDs = req.RuleIDs
	}
	if len(req.Config) > 0 {
		existing.Config = req.Config
	}

	if err := s.store.UpdateAction(r.Context(), existing); err != nil {
		s.logger.Error("api: update action", "action_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update action")
		return
	}

	if err := s.reloadActionRegistry(r.Context()); err != nil {
		s.logger.Error("api: update action: reload registry", "error", err)
	}

	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteAction(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action ID")
		return
	}

	if _, err := s.store.GetAction(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}

	if err := s.store.DeleteAction(r.Context(), id); err != nil {
		s.logger.Error("api: delete action", "action_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete action")
		return
	}

	if err := s.reloadActionRegistry(r.Context()); err != nil {
		s.logger.Error("api: delete action: reload registry", "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateAction(w http.ResponseWriter, r *http.Request) {
	s.setActionActive(w, r, true)
}

func (s *Server) handleDeactivateAction(w http.ResponseWriter, r *http.Request) {
	s.setActionActive(w, r, false)
}

func (s *Server) setActionActive(w http.ResponseWriter, r *http.Request, active bool) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action ID")
		return
	}

	if _, err := s.store.GetAction(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}

	if err := s.store.SetActionActive(r.Context(), id, active); err != nil {
		s.logger.Error("api: set action active", "action_id", id, "active", active, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update action")
		return
	}

	if err := s.reloadActionRegistry(r.Context()); err != nil {
		s.logger.Error("api: set action active: reload registry", "action_id", id, "error", err)
	}

	a, err := s.store.GetAction(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated action")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleListActionExecutions(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action ID")
		return
	}

	filter := store.ExecutionFilter{ActionID: &id}
	applyExecutionQueryParams(r, &filter)

	execs, err := s.store.ListActionExecutions(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list action executions", "action_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list executions")
		return
	}
	writeJSON(w, http.StatusOK, execs)
}

func (s *Server) handleListAllExecutions(w http.ResponseWriter, r *http.Request) {
	filter := store.ExecutionFilter{}
	applyExecutionQueryParams(r, &filter)

	if v := r.URL.Query().Get("tweet_id"); v != "" {
		filter.TweetID = &v
	}
	if v := r.URL.Query().Get("status"); v != "" {
		filter.Status = &v
	}

	execs, err := s.store.ListActionExecutions(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list all executions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list executions")
		return
	}
	writeJSON(w, http.StatusOK, execs)
}

func applyExecutionQueryParams(r *http.Request, filter *store.ExecutionFilter) {
	q := r.URL.Query()
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Page = n
		}
	}
	if v := q.Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.PerPage = n
		}
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.To = &t
		}
	}
}

func (s *Server) handleRunAction(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid action ID")
		return
	}

	a, err := s.store.GetAction(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}
	if !a.Active {
		writeError(w, http.StatusUnprocessableEntity, "action is not active")
		return
	}

	// Collect tweets matching the action's rule filters (or all tweets if none).
	tweets, err := s.tweetsForAction(r.Context(), a)
	if err != nil {
		s.logger.Error("api: run action: fetch tweets", "action_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch tweets")
		return
	}

	count := 0
	for _, tweet := range tweets {
		st := &model.StreamTweet{
			Tweet:         tweet,
			TargetActions: []int64{a.ID},
		}
		if err := s.producer.Push(r.Context(), queue.QueueRaw, st); err != nil {
			s.logger.Error("api: run action: push", "tweet_id", tweet.ID, "error", err)
			continue
		}
		count++
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"queued": count})
}

// tweetsForAction returns all tweets that should be processed by the given action.
// If the action has rule_ids, only tweets matching those rules are returned.
func (s *Server) tweetsForAction(ctx context.Context, a *model.Action) ([]*model.Tweet, error) {
	const pageSize = 5000

	if len(a.RuleIDs) == 0 {
		return s.store.ListTweets(ctx, store.TweetFilter{Page: 1, PerPage: pageSize})
	}

	seen := make(map[string]struct{})
	var result []*model.Tweet
	for _, ruleID := range a.RuleIDs {
		id := ruleID
		tweets, err := s.store.ListTweets(ctx, store.TweetFilter{RuleID: &id, Page: 1, PerPage: pageSize})
		if err != nil {
			return nil, err
		}
		for _, t := range tweets {
			if _, dup := seen[t.ID]; !dup {
				seen[t.ID] = struct{}{}
				result = append(result, t)
			}
		}
	}
	return result, nil
}

// reloadActionRegistry reloads the action registry from the store.
// Called after any action or model change.
func (s *Server) reloadActionRegistry(ctx context.Context) error {
	if s.actionRegistry == nil {
		return nil
	}
	actions, err := s.buildActions(ctx)
	if err != nil {
		return err
	}
	s.actionRegistry.Reload(actions)
	return nil
}

// buildActions loads active actions from the store and constructs Action instances.
func (s *Server) buildActions(ctx context.Context) ([]action.Action, error) {
	active := true
	dbActions, err := s.store.ListActions(ctx, store.ActionFilter{Active: &active})
	if err != nil {
		return nil, err
	}

	out := make([]action.Action, 0, len(dbActions))
	for _, a := range dbActions {
		act, err := s.constructAction(ctx, a)
		if err != nil {
			s.logger.Warn("api: build actions: skip action",
				"action_id", a.ID, "name", a.Name, "error", err)
			continue
		}
		out = append(out, act)
	}
	return out, nil
}

// constructAction builds the concrete Action for a model.Action record.
func (s *Server) constructAction(ctx context.Context, a *model.Action) (action.Action, error) {
	switch a.Type {
	case model.ActionTypePipeline:
		var cfg struct {
			ModelID int64 `json:"model_id"`
		}
		if err := json.Unmarshal(a.Config, &cfg); err != nil {
			return nil, err
		}
		mdl, err := s.store.GetModel(ctx, cfg.ModelID)
		if err != nil {
			return nil, err
		}
		p := s.registry.Get(mdl.Name)
		if p == nil {
			return nil, errorf("model %q not loaded in registry", mdl.Name)
		}
		return action.NewPipelineAction(
			a.ID, a.Name, a.RuleIDs, p,
			s.store, s.producer,
			nil, // emitter not available here; will be nil
			s.registry,
			s.logger,
		), nil

	case model.ActionTypeRetweet, model.ActionTypeLike, model.ActionTypeQuote:
		if s.twitterClient == nil {
			return nil, errorf("Twitter client not configured")
		}
		var cfg action.TwitterConfig
		if err := json.Unmarshal(a.Config, &cfg); err != nil {
			return nil, err
		}
		return action.NewTwitterAction(
			a.ID, a.Name, a.Type, a.RuleIDs, cfg,
			s.twitterClient,
			s.registry,
			s.store,
			s.logger,
		), nil

	default:
		return nil, errorf("unknown action type: %s", a.Type)
	}
}

func errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
