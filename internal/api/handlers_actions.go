package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
	filter := store.ActionFilter{}

	if v := r.URL.Query().Get("trigger_type"); v != "" {
		t := model.TriggerType(v)
		filter.TriggerType = &t
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
		Name        string            `json:"name"`
		TriggerType model.TriggerType `json:"trigger_type"`
		PipelineID  int64             `json:"pipeline_id"`
		RuleIDs     []int64           `json:"rule_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	switch req.TriggerType {
	case model.TriggerTypeNewTweet, model.TriggerTypeNewCategorization, model.TriggerTypeEntityMatched, model.TriggerTypeChainOnly, model.TriggerTypeDigestRequested:
	default:
		writeError(w, http.StatusBadRequest, "invalid trigger_type")
		return
	}
	if req.PipelineID == 0 {
		writeError(w, http.StatusBadRequest, "pipeline_id is required")
		return
	}

	if req.RuleIDs == nil {
		req.RuleIDs = []int64{}
	}

	now := time.Now()
	a := &model.Action{
		Name:        req.Name,
		TriggerType: req.TriggerType,
		PipelineID:  req.PipelineID,
		Active:      true,
		RuleIDs:     req.RuleIDs,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.SaveAction(r.Context(), a); err != nil {
		s.logger.Error("api: create action", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create action")
		return
	}

	if err := s.reloadActionCache(r.Context()); err != nil {
		s.logger.Error("api: create action: reload cache", "error", err)
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
		Name    string  `json:"name"`
		RuleIDs []int64 `json:"rule_ids"`
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

	if err := s.store.UpdateAction(r.Context(), existing); err != nil {
		s.logger.Error("api: update action", "action_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update action")
		return
	}

	if err := s.reloadActionCache(r.Context()); err != nil {
		s.logger.Error("api: update action: reload cache", "error", err)
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.reloadActionCache(r.Context()); err != nil {
		s.logger.Error("api: delete action: reload cache", "error", err)
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

	if err := s.reloadActionCache(r.Context()); err != nil {
		s.logger.Error("api: set action active: reload cache", "action_id", id, "error", err)
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
		ev, _ := json.Marshal(queue.TweetEvent{
			TweetID:      tweet.ID,
			Text:         tweet.Text,
			AuthorHandle: tweet.AuthorHandle,
		})
		task := &queue.Task{
			TriggerType: string(a.TriggerType),
			Event:       ev,
			ActionID:    a.ID,
			Context:     map[string]any{},
		}
		if err := s.producer.PushTask(r.Context(), task); err != nil {
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

// handleGetExecutionMetadata returns the metadata key-value pairs for a single execution.
func (s *Server) handleGetExecutionMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid execution ID")
		return
	}
	meta, err := s.store.ListActionExecutionMetadata(r.Context(), id)
	if err != nil {
		s.logger.Error("api: get execution metadata", "execution_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get execution metadata")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// reloadActionCache reloads the action cache from the store.
// Called after any action change.
func (s *Server) reloadActionCache(ctx context.Context) error {
	if s.actionCache == nil {
		return nil
	}
	return s.actionCache.Load(ctx)
}

