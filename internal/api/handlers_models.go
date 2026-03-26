package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// repoIDPattern validates HuggingFace repo IDs: org/model format with safe characters only.
var repoIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.ListModels(r.Context())
	if err != nil {
		s.logger.Error("api: list models", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list models")
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string          `json:"name"`
		Version string          `json:"version"`
		Path    string          `json:"path"`
		Type    model.ModelType `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	m := &model.Model{
		Name:    req.Name,
		Version: req.Version,
		Path:    req.Path,
		Type:    req.Type,
	}

	if err := s.store.SaveModel(r.Context(), m); err != nil {
		s.logger.Error("api: save model", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save model")
		return
	}

	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid model ID")
		return
	}

	if _, err := s.store.GetModel(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "model not found")
		return
	}

	if err := s.store.DeleteModel(r.Context(), id); err != nil {
		s.logger.Error("api: delete model", "model_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete model")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateModel(w http.ResponseWriter, r *http.Request) {
	s.setModelActive(w, r, true)
}

func (s *Server) handleDeactivateModel(w http.ResponseWriter, r *http.Request) {
	s.setModelActive(w, r, false)
}

func (s *Server) setModelActive(w http.ResponseWriter, r *http.Request, active bool) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid model ID")
		return
	}

	if _, err := s.store.GetModel(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "model not found")
		return
	}

	if err := s.store.SetModelActive(r.Context(), id, active); err != nil {
		s.logger.Error("api: set model active", "model_id", id, "active", active, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update model")
		return
	}

	// Reload registry with current active models.
	if s.registry != nil {
		if reloadErr := s.reloadRegistry(r.Context()); reloadErr != nil {
			s.logger.Error("api: set model active: registry reload", "model_id", id, "error", reloadErr)
			writeError(w, http.StatusInternalServerError, "failed to reload model registry")
			return
		}
	}

	m, err := s.store.GetModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated model")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// reloadRegistry re-reads all active models from the store and reloads the registry.
func (s *Server) reloadRegistry(ctx context.Context) error {
	models, err := s.store.ListModels(ctx)
	if err != nil {
		return err
	}
	activeModels := filterActive(models)
	return s.registry.Reload(ctx, activeModels, func(m *model.Model) (inference.Pipeline, error) {
		switch m.Type {
		case model.ModelTypeSummarizer:
			p := inference.NewSeq2SeqPipeline(m.Name)
			return p, p.Load(m.Path)
		case model.ModelTypeExtraction:
			prompt := inference.OllamaGenericSystem
			if m.Name == "geoconfirmed-extractor" {
				prompt = inference.OllamaGeoConfirmedSystem
			}
			p := inference.NewOllamaPipeline(m.Name, s.ollamaBaseURL, prompt)
			return p, p.Load(m.Path)
		case model.ModelTypeDigest:
			p := inference.NewOllamaPipeline(m.Name, s.ollamaBaseURL, inference.OllamaDigestSystem)
			return p, p.Load(m.Path)
		default:
			p := inference.NewONNXPipeline(m.Name)
			if err := p.Load(m.Path); err != nil {
				return nil, err
			}
			if labels, labelsErr := s.store.ListModelLabels(ctx, m.Name); labelsErr == nil && len(labels) > 0 {
				p.SetLabels(labels)
			}
			return p, nil
		}
	})
}

// filterActive returns only models with Active == true.
func filterActive(models []*model.Model) []*model.Model {
	out := make([]*model.Model, 0, len(models))
	for _, m := range models {
		if m.Active {
			out = append(out, m)
		}
	}
	return out
}

func (s *Server) handleDownloadModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepoID string `json:"repo_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !repoIDPattern.MatchString(req.RepoID) {
		writeError(w, http.StatusBadRequest, "invalid repo_id: must match org/model with safe characters only")
		return
	}

	localDir := filepath.Join("./models", req.RepoID)
	cmd := exec.CommandContext(
		r.Context(),
		"hf", "download", req.RepoID,
		"--include", "*.onnx",
		"--local-dir", localDir,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("api: hf download", "repo_id", req.RepoID, "output", string(out), "error", err)
		writeError(w, http.StatusInternalServerError, "model download failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"repo_id":   req.RepoID,
		"local_dir": localDir,
		"status":    "downloaded",
	})
}

func (s *Server) handleListModelLabels(w http.ResponseWriter, r *http.Request) {
	m, ok := s.resolveModel(w, r)
	if !ok {
		return
	}
	labels, err := s.store.ListModelLabels(r.Context(), m.Name)
	if err != nil {
		s.logger.Error("api: list model labels", "model_id", m.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list labels")
		return
	}
	if labels == nil {
		labels = []string{}
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Server) handleAddModelLabel(w http.ResponseWriter, r *http.Request) {
	m, ok := s.resolveModel(w, r)
	if !ok {
		return
	}
	var req struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Label == "" {
		writeError(w, http.StatusBadRequest, "label is required")
		return
	}
	if err := s.store.AddModelLabel(r.Context(), m.Name, req.Label); err != nil {
		s.logger.Error("api: add model label", "model_id", m.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add label")
		return
	}
	labels, _ := s.store.ListModelLabels(r.Context(), m.Name)
	if s.registry != nil {
		s.registry.SetLabels(m.Name, labels)
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Server) handleRemoveModelLabel(w http.ResponseWriter, r *http.Request) {
	m, ok := s.resolveModel(w, r)
	if !ok {
		return
	}
	label := chi.URLParam(r, "label")
	if label == "" {
		writeError(w, http.StatusBadRequest, "label is required")
		return
	}
	if err := s.store.RemoveModelLabel(r.Context(), m.Name, label); err != nil {
		s.logger.Error("api: remove model label", "model_id", m.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to remove label")
		return
	}
	labels, _ := s.store.ListModelLabels(r.Context(), m.Name)
	if labels == nil {
		labels = []string{}
	}
	if s.registry != nil {
		s.registry.SetLabels(m.Name, labels)
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Server) handleAddModelRule(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid model ID")
		return
	}

	var req struct {
		RuleID int64 `json:"rule_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RuleID == 0 {
		writeError(w, http.StatusBadRequest, "rule_id is required")
		return
	}

	if err := s.store.AddModelRule(r.Context(), id, req.RuleID); err != nil {
		s.logger.Error("api: add model rule", "model_id", id, "rule_id", req.RuleID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add rule association")
		return
	}

	if s.registry != nil {
		if reloadErr := s.reloadRegistry(r.Context()); reloadErr != nil {
			s.logger.Error("api: add model rule: registry reload", "model_id", id, "error", reloadErr)
			writeError(w, http.StatusInternalServerError, "failed to reload model registry")
			return
		}
	}

	m, err := s.store.GetModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated model")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleRemoveModelRule(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid model ID")
		return
	}

	ruleIDStr := chi.URLParam(r, "rule_id")
	ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	if err := s.store.RemoveModelRule(r.Context(), id, ruleID); err != nil {
		s.logger.Error("api: remove model rule", "model_id", id, "rule_id", ruleID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to remove rule association")
		return
	}

	if s.registry != nil {
		if reloadErr := s.reloadRegistry(r.Context()); reloadErr != nil {
			s.logger.Error("api: remove model rule: registry reload", "model_id", id, "error", reloadErr)
			writeError(w, http.StatusInternalServerError, "failed to reload model registry")
			return
		}
	}

	m, err := s.store.GetModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated model")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleRunModelPipeline(w http.ResponseWriter, r *http.Request) {
	m, ok := s.resolveModel(w, r)
	if !ok {
		return
	}

	if s.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "registry not available")
		return
	}

	pipeline := s.registry.Get(m.Name)
	if pipeline == nil {
		writeError(w, http.StatusUnprocessableEntity, "model is not active or not loaded in the registry")
		return
	}

	// Collect tweets matching the model's rule filters (or all tweets if none).
	tweets, err := s.tweetsForModel(r.Context(), m)
	if err != nil {
		s.logger.Error("api: run model: fetch tweets", "model", m.Name, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch tweets")
		return
	}

	// Push each tweet into the normal worker queue, tagged so workers run only
	// this model. The queue gives visibility in the UI and respects the pool.
	count := 0
	for _, tweet := range tweets {
		st := &model.StreamTweet{
			Tweet:        tweet,
			TargetModels: []string{pipeline.Name()},
		}
		if err := s.producer.Push(r.Context(), queue.QueueRaw, st); err != nil {
			s.logger.Error("api: run model: push", "tweet_id", tweet.ID, "error", err)
			continue
		}
		count++
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"queued": count})
}

// tweetsForModel returns all tweets that should be processed by the given model.
// If the model has rule_ids, only tweets that matched at least one of those rules
// are returned. If no rules are set, all tweets are returned.
func (s *Server) tweetsForModel(ctx context.Context, m *model.Model) ([]*model.Tweet, error) {
	const pageSize = 5000

	if len(m.RuleIDs) == 0 {
		// Global model — run on all tweets.
		return s.store.ListTweets(ctx, store.TweetFilter{Page: 1, PerPage: pageSize})
	}

	// Fetch tweets per rule and deduplicate by tweet ID.
	seen := make(map[string]struct{})
	var result []*model.Tweet
	for _, ruleID := range m.RuleIDs {
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
func (s *Server) resolveModel(w http.ResponseWriter, r *http.Request) (*model.Model, bool) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid model ID")
		return nil, false
	}
	m, err := s.store.GetModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "model not found")
		return nil, false
	}
	return m, true
}
