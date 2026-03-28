package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListChains(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	chains, err := s.store.ListActionChains(r.Context(), store.ActionChainFilter{SourceActionID: &id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, chains)
}

func (s *Server) handleCreateChain(w http.ResponseWriter, r *http.Request) {
	sourceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var c model.ActionChain
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	c.SourceActionID = sourceID
	if err := s.store.SaveActionChain(r.Context(), &c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateChain(w http.ResponseWriter, r *http.Request) {
	chainID, err := strconv.ParseInt(chi.URLParam(r, "chain_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chain_id")
		return
	}
	var c model.ActionChain
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	c.ID = chainID
	if err := s.store.UpdateActionChain(r.Context(), &c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteChain(w http.ResponseWriter, r *http.Request) {
	chainID, err := strconv.ParseInt(chi.URLParam(r, "chain_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chain_id")
		return
	}
	if err := s.store.DeleteActionChain(r.Context(), chainID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
