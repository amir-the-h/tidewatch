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

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// repoIDPattern validates HuggingFace repo IDs: org/model format with safe characters only.
var repoIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

// handleListModels is deprecated. Use GET /api/pipelines instead.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/pipelines")
}

// handleCreateModel is deprecated. Use POST /api/pipelines instead.
func (s *Server) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/pipelines")
}

// handleDeleteModel is deprecated. Use DELETE /api/pipelines/{id} instead.
func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/pipelines")
}

// handleActivateModel is deprecated. Use POST /api/pipelines/{id}/activate instead.
func (s *Server) handleActivateModel(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/pipelines")
}

// handleDeactivateModel is deprecated. Use POST /api/pipelines/{id}/deactivate instead.
func (s *Server) handleDeactivateModel(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/pipelines")
}

// handleListModelLabels is deprecated. Use GET /api/pipelines/{id}/labels instead.
func (s *Server) handleListModelLabels(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/pipelines")
}

// handleAddModelLabel is deprecated. Use POST /api/pipelines/{id}/labels instead.
func (s *Server) handleAddModelLabel(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/pipelines")
}

// handleRemoveModelLabel is deprecated. Use DELETE /api/pipelines/{id}/labels/{label} instead.
func (s *Server) handleRemoveModelLabel(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/pipelines")
}

// handleAddModelRule is deprecated. Rule associations are now managed via actions.
func (s *Server) handleAddModelRule(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/actions")
}

// handleRemoveModelRule is deprecated. Rule associations are now managed via actions.
func (s *Server) handleRemoveModelRule(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "models API is deprecated; use /api/actions")
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

func (s *Server) handleRunModelPipeline(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid model ID")
		return
	}

	if s.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "registry not available")
		return
	}

	// Resolve model name from the registry by looking up all pipelines and
	// matching by ID — the registry holds pipelines by name, so we need to
	// find which name corresponds to this ID via the store.
	// Since models are deprecated we do a best-effort lookup via pipeline store.
	pipeline, err := s.store.GetPipeline(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "model/pipeline not found")
		return
	}

	p := s.registry.Get(pipeline.Name)
	if p == nil {
		writeError(w, http.StatusUnprocessableEntity, "model is not active or not loaded in the registry")
		return
	}

	// Collect tweets matching the pipeline's rule filters (or all tweets if none).
	tweets, err := s.tweetsForPipeline(r.Context(), pipeline)
	if err != nil {
		s.logger.Error("api: run model: fetch tweets", "pipeline", pipeline.Name, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch tweets")
		return
	}

	// Find active actions backed by this pipeline name via the action cache.
	var actionIDs []int64
	if s.actionCache != nil {
		entries := s.actionCache.AllForRules(nil)
		for _, e := range entries {
			if e.Pipeline != nil && e.Pipeline.Name() == pipeline.Name {
				actionIDs = append(actionIDs, e.Action.ID)
			}
		}
	}

	// Push tasks for each tweet to each matching action.
	count := 0
	for _, tweet := range tweets {
		ev, _ := json.Marshal(queue.TweetEvent{
			TweetID:      tweet.ID,
			Text:         tweet.Text,
			AuthorHandle: tweet.AuthorHandle,
		})
		pushed := false
		for _, actionID := range actionIDs {
			task := &queue.Task{
				TriggerType: string(model.TriggerTypeNewTweet),
				Event:       ev,
				ActionID:    actionID,
				Context:     map[string]any{},
			}
			if err := s.producer.PushTask(r.Context(), task); err != nil {
				s.logger.Error("api: run model: push task", "tweet_id", tweet.ID, "action_id", actionID, "error", err)
			} else {
				pushed = true
			}
		}
		if pushed {
			count++
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"queued": count})
}

// tweetsForPipeline returns all tweets to be processed by the given pipeline.
func (s *Server) tweetsForPipeline(ctx context.Context, p *model.Pipeline) ([]*model.Tweet, error) {
	const pageSize = 5000
	return s.store.ListTweets(ctx, store.TweetFilter{Page: 1, PerPage: pageSize})
}
