package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// ── NotificationProviders ─────────────────────────────────────────────────────

func (s *Server) handleListNotificationProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.ListNotificationProviders(r.Context())
	if err != nil {
		s.logger.Error("api: list notification providers", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list notification providers")
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) handleCreateNotificationProvider(w http.ResponseWriter, r *http.Request) {
	var p model.NotificationProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if p.Type != "pushover" && p.Type != "telegram" && p.Type != "discord" {
		writeError(w, http.StatusBadRequest, "type must be \"pushover\", \"telegram\", or \"discord\"")
		return
	}
	if len(p.Config) == 0 {
		writeError(w, http.StatusBadRequest, "config is required")
		return
	}

	p.Active = true
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now

	if err := s.store.SaveNotificationProvider(r.Context(), &p); err != nil {
		s.logger.Error("api: save notification provider", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save notification provider")
		return
	}
	if list, err := s.store.ListNotificationProviders(r.Context()); err == nil {
		s.broadcastEvent("notification_providers", list)
	}
	writeJSON(w, http.StatusCreated, &p)
}

func (s *Server) handleGetNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetNotificationProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification provider not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetNotificationProvider(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification provider not found")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if p.Type != "pushover" && p.Type != "telegram" && p.Type != "discord" {
		writeError(w, http.StatusBadRequest, "type must be \"pushover\", \"telegram\", or \"discord\"")
		return
	}
	p.UpdatedAt = time.Now()
	if err := s.store.UpdateNotificationProvider(r.Context(), p); err != nil {
		s.logger.Error("api: update notification provider", "provider_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update notification provider")
		return
	}
	s.reloadCache(r.Context())
	if list, err := s.store.ListNotificationProviders(r.Context()); err == nil {
		s.broadcastEvent("notification_providers", list)
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteNotificationProvider(r.Context(), id); err != nil {
		s.logger.Error("api: delete notification provider", "provider_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete notification provider")
		return
	}
	if list, err := s.store.ListNotificationProviders(r.Context()); err == nil {
		s.broadcastEvent("notification_providers", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetNotificationProviderActive(r.Context(), id, true); err != nil {
		s.logger.Error("api: activate notification provider", "provider_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to activate notification provider")
		return
	}
	s.reloadCache(r.Context())
	if list, err := s.store.ListNotificationProviders(r.Context()); err == nil {
		s.broadcastEvent("notification_providers", list)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}

func (s *Server) handleDeactivateNotificationProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationProviderID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetNotificationProviderActive(r.Context(), id, false); err != nil {
		s.logger.Error("api: deactivate notification provider", "provider_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to deactivate notification provider")
		return
	}
	s.reloadCache(r.Context())
	if list, err := s.store.ListNotificationProviders(r.Context()); err == nil {
		s.broadcastEvent("notification_providers", list)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

// ── NotificationChannels ──────────────────────────────────────────────────────

func (s *Server) handleListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	var providerID *int64
	if v := r.URL.Query().Get("provider_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid provider_id")
			return
		}
		providerID = &id
	}
	channels, err := s.store.ListNotificationChannels(r.Context(), providerID)
	if err != nil {
		s.logger.Error("api: list notification channels", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list notification channels")
		return
	}
	writeJSON(w, http.StatusOK, channels)
}

func (s *Server) handleCreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	var c model.NotificationChannel
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if c.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if c.ProviderID == 0 {
		writeError(w, http.StatusBadRequest, "provider_id is required")
		return
	}
	// Verify provider exists.
	if _, err := s.store.GetNotificationProvider(r.Context(), c.ProviderID); err != nil {
		writeError(w, http.StatusBadRequest, "notification provider not found")
		return
	}

	c.Active = true
	now := time.Now()
	c.CreatedAt = now
	c.UpdatedAt = now

	if err := s.store.SaveNotificationChannel(r.Context(), &c); err != nil {
		s.logger.Error("api: save notification channel", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save notification channel")
		return
	}
	if list, err := s.store.ListNotificationChannels(r.Context(), nil); err == nil {
		s.broadcastEvent("notification_channels", list)
	}
	writeJSON(w, http.StatusCreated, &c)
}

func (s *Server) handleGetNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	c, err := s.store.GetNotificationChannel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification channel not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleUpdateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	c, err := s.store.GetNotificationChannel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification channel not found")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if c.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	c.UpdatedAt = time.Now()
	if err := s.store.UpdateNotificationChannel(r.Context(), c); err != nil {
		s.logger.Error("api: update notification channel", "channel_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update notification channel")
		return
	}
	s.reloadCache(r.Context())
	if list, err := s.store.ListNotificationChannels(r.Context(), nil); err == nil {
		s.broadcastEvent("notification_channels", list)
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteNotificationChannel(r.Context(), id); err != nil {
		s.logger.Error("api: delete notification channel", "channel_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete notification channel")
		return
	}
	if list, err := s.store.ListNotificationChannels(r.Context(), nil); err == nil {
		s.broadcastEvent("notification_channels", list)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetNotificationChannelActive(r.Context(), id, true); err != nil {
		s.logger.Error("api: activate notification channel", "channel_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to activate notification channel")
		return
	}
	s.reloadCache(r.Context())
	if list, err := s.store.ListNotificationChannels(r.Context(), nil); err == nil {
		s.broadcastEvent("notification_channels", list)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}

func (s *Server) handleDeactivateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationChannelID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetNotificationChannelActive(r.Context(), id, false); err != nil {
		s.logger.Error("api: deactivate notification channel", "channel_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to deactivate notification channel")
		return
	}
	s.reloadCache(r.Context())
	if list, err := s.store.ListNotificationChannels(r.Context(), nil); err == nil {
		s.broadcastEvent("notification_channels", list)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func parseNotificationProviderID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification provider ID")
		return 0, false
	}
	return id, true
}

func parseNotificationChannelID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification channel ID")
		return 0, false
	}
	return id, true
}

// handleBulkDeleteNotificationProviders DELETE /api/notification-providers
func (s *Server) handleBulkDeleteNotificationProviders(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteNotificationProvidersFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteNotificationProviders(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete notification providers", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete notification providers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

// handleBulkDeleteNotificationChannels DELETE /api/notification-channels
func (s *Server) handleBulkDeleteNotificationChannels(w http.ResponseWriter, r *http.Request) {
	var f store.BulkDeleteNotificationChannelsFilter
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n, err := s.store.BulkDeleteNotificationChannels(r.Context(), f)
	if err != nil {
		s.logger.Error("api: bulk delete notification channels", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to bulk delete notification channels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
