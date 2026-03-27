package api

import (
	"net/http"
)

func (s *Server) handleTwitterAuthStart(w http.ResponseWriter, r *http.Request) {
	if s.twitterClient == nil {
		writeError(w, http.StatusServiceUnavailable, "Twitter client not configured")
		return
	}

	authURL, _, err := s.twitterClient.StartAuth()
	if err != nil {
		s.logger.Error("api: twitter auth start", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start Twitter auth")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

func (s *Server) handleTwitterAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.twitterClient == nil {
		writeError(w, http.StatusServiceUnavailable, "Twitter client not configured")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state")
		return
	}

	if err := s.twitterClient.HandleCallback(r.Context(), code, state); err != nil {
		s.logger.Error("api: twitter auth callback", "error", err)
		writeError(w, http.StatusBadRequest, "OAuth callback failed")
		return
	}

	// Redirect to the settings page after successful auth.
	http.Redirect(w, r, "/actions", http.StatusFound)
}

func (s *Server) handleTwitterAuthStatus(w http.ResponseWriter, r *http.Request) {
	if s.twitterClient == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	creds, err := s.twitterClient.Status(r.Context())
	if err != nil {
		s.logger.Error("api: twitter auth status", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get Twitter status")
		return
	}
	if creds == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, creds)
}

func (s *Server) handleTwitterDisconnect(w http.ResponseWriter, r *http.Request) {
	if s.twitterClient == nil {
		writeError(w, http.StatusServiceUnavailable, "Twitter client not configured")
		return
	}

	if err := s.twitterClient.Disconnect(r.Context()); err != nil {
		s.logger.Error("api: twitter disconnect", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to disconnect Twitter account")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
