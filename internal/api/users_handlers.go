package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/auth"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// handleListUsers returns all users. Admin only.
// GET /api/users
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.logger.Error("users: list", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if users == nil {
		users = []*model.User{}
	}
	writeJSON(w, http.StatusOK, users)
}

// handleCreateUser creates a new user. Admin only.
// POST /api/users
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if req.Role != model.RoleAdmin && req.Role != model.RoleViewer {
		req.Role = model.RoleViewer
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.logger.Error("users: hash password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	now := time.Now()
	u := &model.User{
		Username:     req.Username,
		PasswordHash: hash,
		Role:         req.Role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.SaveUser(r.Context(), u); err != nil {
		writeError(w, http.StatusConflict, "username already exists")
		return
	}
	if list, err := s.store.ListUsers(r.Context()); err == nil {
		s.broadcastEvent("users", list)
	}
	writeJSON(w, http.StatusCreated, u)
}

// handleUpdateUser updates a user's password or role. Admin only.
// PUT /api/users/{id}
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	u, err := s.store.GetUser(r.Context(), id)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// Prevent downgrading the last admin.
	if u.Role == model.RoleAdmin && req.Role == model.RoleViewer {
		count, err := s.store.CountAdmins(r.Context())
		if err != nil || count <= 1 {
			writeError(w, http.StatusConflict, "cannot remove the last admin")
			return
		}
	}

	if req.Role == model.RoleAdmin || req.Role == model.RoleViewer {
		u.Role = req.Role
	}
	if req.Password != "" {
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		u.PasswordHash = hash
	}

	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		s.logger.Error("users: update", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if list, err := s.store.ListUsers(r.Context()); err == nil {
		s.broadcastEvent("users", list)
	}
	writeJSON(w, http.StatusOK, u)
}

// handleDeleteUser deletes a user. Admin only. Cannot delete the last admin.
// DELETE /api/users/{id}
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	// Prevent self-deletion and deleting the last admin.
	claims := auth.ClaimsFromContext(r.Context())
	if claims != nil && claims.UserID == id {
		writeError(w, http.StatusConflict, "cannot delete your own account")
		return
	}

	u, err := s.store.GetUser(r.Context(), id)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if u.Role == model.RoleAdmin {
		count, err := s.store.CountAdmins(r.Context())
		if err != nil || count <= 1 {
			writeError(w, http.StatusConflict, "cannot delete the last admin")
			return
		}
	}

	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		s.logger.Error("users: delete", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if list, err := s.store.ListUsers(r.Context()); err == nil {
		s.broadcastEvent("users", list)
	}
	w.WriteHeader(http.StatusNoContent)
}
