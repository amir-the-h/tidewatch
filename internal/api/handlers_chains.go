package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

func (s *Server) handleListChains(w http.ResponseWriter, r *http.Request) {
	chains, err := s.store.ListChains(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if chains == nil {
		chains = []*model.Chain{}
	}
	writeJSON(w, http.StatusOK, chains)
}

func (s *Server) handleGetChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	chain, err := s.store.GetChain(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	writeJSON(w, http.StatusOK, chain)
}

func (s *Server) handleCreateChain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	c := &model.Chain{Name: req.Name, Description: req.Description}
	if err := s.store.SaveChain(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	c, err := s.store.GetChain(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	c.Description = req.Description
	if err := s.store.UpdateChain(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteChain(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetChainActive(r.Context(), id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeactivateChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetChainActive(r.Context(), id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetChainGraph(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	graph, err := s.store.GetChainGraph(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (s *Server) handleSaveChainGraph(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req model.ChainGraphRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	graph, err := s.store.SaveChainGraph(r.Context(), id, req.Nodes, req.Edges)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (s *Server) handleRunChain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		NewOnly bool `json:"new_only"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	chain, err := s.store.GetChain(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "chain not found")
		return
	}
	if !chain.Active {
		writeError(w, http.StatusUnprocessableEntity, "chain is not active")
		return
	}

	// Collect active trigger links for this chain.
	allLinks, err := s.store.ListTriggerChainLinks(r.Context())
	if err != nil {
		s.logger.Error("api: run chain: list trigger links", "chain_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list trigger links")
		return
	}
	var links []*model.TriggerChainLink
	for _, l := range allLinks {
		if l.ChainID == id && l.Active {
			links = append(links, l)
		}
	}
	if len(links) == 0 {
		writeJSON(w, http.StatusAccepted, map[string]any{"queued": 0})
		return
	}

	// Load graph and find first-level action nodes.
	graph, err := s.store.GetChainGraph(r.Context(), id)
	if err != nil {
		s.logger.Error("api: run chain: load graph", "chain_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load chain graph")
		return
	}
	actionNodes := worker.FirstLevelActionNodes(graph)
	if len(actionNodes) == 0 {
		writeJSON(w, http.StatusAccepted, map[string]any{"queued": 0})
		return
	}

	// Build exclusion set once if new_only is requested.
	var executedIDs map[string]struct{}
	if req.NewOnly {
		executedIDs = make(map[string]struct{})
		for _, node := range graph.Nodes {
			if node.Type == model.ChainNodeTypeAction && node.ActionID != nil {
				execs, err := s.store.ListActionExecutions(r.Context(), store.ExecutionFilter{ActionID: node.ActionID})
				if err != nil {
					s.logger.Warn("api: run chain: fetch executions", "action_id", *node.ActionID, "error", err)
					continue
				}
				for _, e := range execs {
					executedIDs[e.TweetID] = struct{}{}
				}
			}
		}
	}

	// Push tasks: for each active trigger link, select eligible tweets and dispatch.
	count := 0
	for _, link := range links {
		filter := store.TweetFilter{Page: 1, PerPage: 5000}
		switch link.TriggerType {
		case "new_tweet", "new_categorization":
			// all tweets
		case "entity_matched":
			filter.HasEntityTrack = true
		default:
			continue
		}

		tweets, err := s.store.ListTweets(r.Context(), filter)
		if err != nil {
			s.logger.Error("api: run chain: list tweets", "trigger", link.TriggerType, "error", err)
			continue
		}

		for _, tweet := range tweets {
			if req.NewOnly {
				if _, done := executedIDs[tweet.ID]; done {
					continue
				}
			}
			ev, _ := json.Marshal(queue.TweetEvent{
				TweetID:      tweet.ID,
				Text:         tweet.Text,
				AuthorHandle: tweet.AuthorHandle,
			})
			for _, node := range actionNodes {
				if node.ActionID == nil {
					continue
				}
				task := &queue.Task{
					TriggerType: link.TriggerType,
					Event:       ev,
					ActionID:    *node.ActionID,
					ChainNodeID: node.ID,
					ChainID:     id,
					Context:     map[string]any{},
				}
				if pushErr := s.producer.PushTask(r.Context(), task); pushErr != nil {
					s.logger.Error("api: run chain: push task",
						"chain_id", id, "tweet_id", tweet.ID, "error", pushErr)
					continue
				}
				count++
			}
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"queued": count})
}
