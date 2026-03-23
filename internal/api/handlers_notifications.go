package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	filter := store.NotificationFilter{}

	if et := r.URL.Query().Get("event_type"); et != "" {
		filter.EventType = &et
	}
	if activeStr := r.URL.Query().Get("active"); activeStr != "" {
		active := activeStr == "true"
		filter.Active = &active
	}
	if ruleIDStr := r.URL.Query().Get("rule_id"); ruleIDStr != "" {
		ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid rule_id")
			return
		}
		filter.RuleID = &ruleID
	}

	notifications, err := s.store.ListNotifications(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list notifications", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}
	writeJSON(w, http.StatusOK, notifications)
}

func (s *Server) handleCreateNotification(w http.ResponseWriter, r *http.Request) {
	var n model.Notification
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if n.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if n.EventType == "" {
		writeError(w, http.StatusBadRequest, "event_type is required")
		return
	}
	if !notifier.ValidEventTypes[n.EventType] {
		writeError(w, http.StatusBadRequest, "invalid event_type")
		return
	}
	if n.PushoverPriority == 2 {
		retry := 0
		if n.PushoverRetry != nil {
			retry = *n.PushoverRetry
		}
		expire := 0
		if n.PushoverExpire != nil {
			expire = *n.PushoverExpire
		}
		if retry < 30 {
			writeError(w, http.StatusBadRequest, "pushover_retry must be >= 30 for emergency priority")
			return
		}
		if expire <= 0 || expire > 10800 {
			writeError(w, http.StatusBadRequest, "pushover_expire must be between 1 and 10800 for emergency priority")
			return
		}
	}
	if n.MessageTemplate != nil {
		if err := notifier.ValidateTemplate(*n.MessageTemplate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	n.Active = true
	now := time.Now()
	n.CreatedAt = now
	n.UpdatedAt = now

	if err := s.store.SaveNotification(r.Context(), &n); err != nil {
		s.logger.Error("api: save notification", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save notification")
		return
	}

	writeJSON(w, http.StatusCreated, &n)
}

func (s *Server) handleGetNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationID(w, r)
	if !ok {
		return
	}

	n, err := s.store.GetNotification(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleUpdateNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationID(w, r)
	if !ok {
		return
	}

	n, err := s.store.GetNotification(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}

	if err := json.NewDecoder(r.Body).Decode(n); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if n.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if n.EventType == "" {
		writeError(w, http.StatusBadRequest, "event_type is required")
		return
	}
	if !notifier.ValidEventTypes[n.EventType] {
		writeError(w, http.StatusBadRequest, "invalid event_type")
		return
	}
	if n.PushoverPriority == 2 {
		retry := 0
		if n.PushoverRetry != nil {
			retry = *n.PushoverRetry
		}
		expire := 0
		if n.PushoverExpire != nil {
			expire = *n.PushoverExpire
		}
		if retry < 30 {
			writeError(w, http.StatusBadRequest, "pushover_retry must be >= 30 for emergency priority")
			return
		}
		if expire <= 0 || expire > 10800 {
			writeError(w, http.StatusBadRequest, "pushover_expire must be between 1 and 10800 for emergency priority")
			return
		}
	}
	if n.MessageTemplate != nil {
		if err := notifier.ValidateTemplate(*n.MessageTemplate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	n.UpdatedAt = time.Now()

	if err := s.store.UpdateNotification(r.Context(), n); err != nil {
		s.logger.Error("api: update notification", "notification_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update notification")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleDeleteNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationID(w, r)
	if !ok {
		return
	}

	if err := s.store.DeleteNotification(r.Context(), id); err != nil {
		s.logger.Error("api: delete notification", "notification_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete notification")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationID(w, r)
	if !ok {
		return
	}

	if err := s.store.SetNotificationActive(r.Context(), id, true); err != nil {
		s.logger.Error("api: activate notification", "notification_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to activate notification")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}

func (s *Server) handleDeactivateNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := parseNotificationID(w, r)
	if !ok {
		return
	}

	if err := s.store.SetNotificationActive(r.Context(), id, false); err != nil {
		s.logger.Error("api: deactivate notification", "notification_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to deactivate notification")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	if s.dispatcher == nil {
		writeError(w, http.StatusNotImplemented, "notifier not configured")
		return
	}

	var req struct {
		NotificationID int64 `json:"notification_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.dispatcher.SendTestNotification(r.Context(), req.NotificationID); err != nil {
		s.logger.Error("api: test notification", "notification_id", req.NotificationID, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

// parseNotificationID extracts and parses the {id} path parameter.
// Returns false and writes an error response if parsing fails.
func parseNotificationID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification ID")
		return 0, false
	}
	return id, true
}
