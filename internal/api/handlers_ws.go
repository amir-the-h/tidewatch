package api

import (
	"net/http"

	"github.com/amir-the-h/tidewatch/internal/auth"
	"github.com/amir-the-h/tidewatch/internal/hub"
)

// handleWS upgrades the connection to WebSocket after validating the JWT
// supplied in the "token" query parameter. Auth via query param is required
// because browsers cannot set Authorization headers for WebSocket connections.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	if _, err := auth.ValidateToken(s.jwtSecret, token); err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	if s.hub == nil {
		http.Error(w, "websocket not available", http.StatusServiceUnavailable)
		return
	}
	hub.ServeWS(s.hub, w, r)
}
