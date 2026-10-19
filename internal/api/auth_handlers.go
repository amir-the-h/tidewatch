package api

import (
	"encoding/json"
	"net/http"

	"github.com/amir-the-h/tidewatch/internal/auth"
)

// handleLogin authenticates a user and returns a JWT.
// POST /api/auth/login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	u, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		s.logger.Error("auth: get user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if u == nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.IssueToken(s.jwtSecret, u)
	if err != nil {
		s.logger.Error("auth: issue token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user": map[string]any{
			"id":       u.ID,
			"username": u.Username,
			"role":     u.Role,
		},
	})
}

// handleMe returns the current authenticated user's info.
// GET /api/auth/me
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       claims.UserID,
		"username": claims.Username,
		"role":     claims.Role,
	})
}
