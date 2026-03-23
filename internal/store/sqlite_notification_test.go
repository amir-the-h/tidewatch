package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func sampleNotification() *model.Notification {
	tmpl := "Tweet: {{.Text}}"
	return &model.Notification{
		Name:            "iran-alert",
		EventType:       "tweet",
		Active:          true,
		PushoverPriority: 0,
		PushoverHTML:    false,
		MessageTemplate: &tmpl,
		CreatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestNotification_SaveGet verifies that a saved notification can be retrieved by ID.
func TestNotification_SaveGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	n := sampleNotification()
	if err := s.SaveNotification(ctx, n); err != nil {
		t.Fatalf("SaveNotification: %v", err)
	}
	if n.ID == 0 {
		t.Fatal("expected non-zero ID after save")
	}

	got, err := s.GetNotification(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if got.ID != n.ID {
		t.Errorf("ID: got %d, want %d", got.ID, n.ID)
	}
	if got.Name != n.Name {
		t.Errorf("Name: got %q, want %q", got.Name, n.Name)
	}
	if got.EventType != n.EventType {
		t.Errorf("EventType: got %q, want %q", got.EventType, n.EventType)
	}
	if got.Active != n.Active {
		t.Errorf("Active: got %v, want %v", got.Active, n.Active)
	}
	if got.MessageTemplate == nil || *got.MessageTemplate != *n.MessageTemplate {
		t.Errorf("MessageTemplate: got %v, want %v", got.MessageTemplate, n.MessageTemplate)
	}
	if !got.CreatedAt.Equal(n.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, n.CreatedAt)
	}
}

// TestNotification_SaveGet_NullableFields verifies nullable/optional fields round-trip correctly.
func TestNotification_SaveGet_NullableFields(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sound := "none"
	device := "phone"
	title := "Alert"
	url := "https://example.com"
	urlTitle := "Details"
	ttl := 3600
	retry := 60
	expire := 600
	raw := json.RawMessage(`{"label":"politics"}`)

	n := sampleNotification()
	n.PushoverSound = &sound
	n.PushoverDevice = &device
	n.PushoverTitle = &title
	n.PushoverURL = &url
	n.PushoverURLTitle = &urlTitle
	n.PushoverTTL = &ttl
	n.PushoverHTML = true
	n.PushoverPriority = 1
	n.PushoverRetry = &retry
	n.PushoverExpire = &expire
	n.FilterModelName = strPtr("classifier")
	n.FilterModelResult = &raw

	if err := s.SaveNotification(ctx, n); err != nil {
		t.Fatalf("SaveNotification: %v", err)
	}

	got, err := s.GetNotification(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}

	if got.PushoverSound == nil || *got.PushoverSound != sound {
		t.Errorf("PushoverSound: got %v, want %q", got.PushoverSound, sound)
	}
	if got.PushoverDevice == nil || *got.PushoverDevice != device {
		t.Errorf("PushoverDevice: got %v, want %q", got.PushoverDevice, device)
	}
	if got.PushoverTitle == nil || *got.PushoverTitle != title {
		t.Errorf("PushoverTitle: got %v, want %q", got.PushoverTitle, title)
	}
	if got.PushoverURL == nil || *got.PushoverURL != url {
		t.Errorf("PushoverURL: got %v, want %q", got.PushoverURL, url)
	}
	if got.PushoverURLTitle == nil || *got.PushoverURLTitle != urlTitle {
		t.Errorf("PushoverURLTitle: got %v, want %q", got.PushoverURLTitle, urlTitle)
	}
	if got.PushoverTTL == nil || *got.PushoverTTL != ttl {
		t.Errorf("PushoverTTL: got %v, want %d", got.PushoverTTL, ttl)
	}
	if !got.PushoverHTML {
		t.Error("PushoverHTML: expected true")
	}
	if got.PushoverPriority != 1 {
		t.Errorf("PushoverPriority: got %d, want 1", got.PushoverPriority)
	}
	if got.PushoverRetry == nil || *got.PushoverRetry != retry {
		t.Errorf("PushoverRetry: got %v, want %d", got.PushoverRetry, retry)
	}
	if got.PushoverExpire == nil || *got.PushoverExpire != expire {
		t.Errorf("PushoverExpire: got %v, want %d", got.PushoverExpire, expire)
	}
	if got.FilterModelName == nil || *got.FilterModelName != "classifier" {
		t.Errorf("FilterModelName: got %v, want classifier", got.FilterModelName)
	}
	if got.FilterModelResult == nil || string(*got.FilterModelResult) != string(raw) {
		t.Errorf("FilterModelResult: got %v, want %s", got.FilterModelResult, raw)
	}
}

// TestNotification_GetNotFound verifies that getting a missing notification returns sql.ErrNoRows.
func TestNotification_GetNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.GetNotification(ctx, 999)
	if err == nil {
		t.Fatal("expected error for missing notification")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows wrapped, got: %v", err)
	}
}

// TestNotification_ListFilters verifies ListNotifications with EventType, Active, and RuleID filters.
func TestNotification_ListFilters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Save a rule so we can use its ID as FK
	rule := sampleRule()
	if err := s.SaveRule(ctx, rule); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}

	n1 := sampleNotification()
	n1.EventType = "tweet"
	n1.Active = true
	n1.FilterRuleID = &rule.ID
	if err := s.SaveNotification(ctx, n1); err != nil {
		t.Fatalf("SaveNotification n1: %v", err)
	}

	n2 := sampleNotification()
	n2.Name = "other-alert"
	n2.EventType = "retweet"
	n2.Active = false
	if err := s.SaveNotification(ctx, n2); err != nil {
		t.Fatalf("SaveNotification n2: %v", err)
	}

	// Filter by EventType
	results, err := s.ListNotifications(ctx, NotificationFilter{EventType: strPtr("tweet")})
	if err != nil {
		t.Fatalf("ListNotifications by EventType: %v", err)
	}
	if len(results) != 1 || results[0].ID != n1.ID {
		t.Errorf("expected 1 tweet notification, got %d", len(results))
	}

	// Filter by Active=true
	trueVal := true
	results, err = s.ListNotifications(ctx, NotificationFilter{Active: &trueVal})
	if err != nil {
		t.Fatalf("ListNotifications by Active: %v", err)
	}
	if len(results) != 1 || !results[0].Active {
		t.Errorf("expected 1 active notification, got %d", len(results))
	}

	// Filter by RuleID
	results, err = s.ListNotifications(ctx, NotificationFilter{RuleID: &rule.ID})
	if err != nil {
		t.Fatalf("ListNotifications by RuleID: %v", err)
	}
	if len(results) != 1 || results[0].ID != n1.ID {
		t.Errorf("expected 1 notification with rule ID, got %d", len(results))
	}

	// No filter — all
	results, err = s.ListNotifications(ctx, NotificationFilter{})
	if err != nil {
		t.Fatalf("ListNotifications no filter: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 notifications, got %d", len(results))
	}
}

// TestNotification_Update verifies that UpdateNotification persists field changes.
func TestNotification_Update(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	n := sampleNotification()
	if err := s.SaveNotification(ctx, n); err != nil {
		t.Fatalf("SaveNotification: %v", err)
	}

	n.Name = "updated-alert"
	n.EventType = "retweet"
	n.PushoverPriority = 2
	n.PushoverHTML = true
	n.UpdatedAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := s.UpdateNotification(ctx, n); err != nil {
		t.Fatalf("UpdateNotification: %v", err)
	}

	got, err := s.GetNotification(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotification after update: %v", err)
	}
	if got.Name != "updated-alert" {
		t.Errorf("Name: got %q, want updated-alert", got.Name)
	}
	if got.EventType != "retweet" {
		t.Errorf("EventType: got %q, want retweet", got.EventType)
	}
	if got.PushoverPriority != 2 {
		t.Errorf("PushoverPriority: got %d, want 2", got.PushoverPriority)
	}
	if !got.PushoverHTML {
		t.Error("PushoverHTML: expected true after update")
	}
	if !got.UpdatedAt.Equal(n.UpdatedAt) {
		t.Errorf("UpdatedAt: got %v, want %v", got.UpdatedAt, n.UpdatedAt)
	}
}

// TestNotification_Delete verifies that a deleted notification is no longer retrievable.
func TestNotification_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	n := sampleNotification()
	if err := s.SaveNotification(ctx, n); err != nil {
		t.Fatalf("SaveNotification: %v", err)
	}

	if err := s.DeleteNotification(ctx, n.ID); err != nil {
		t.Fatalf("DeleteNotification: %v", err)
	}

	_, err := s.GetNotification(ctx, n.ID)
	if err == nil {
		t.Fatal("expected error after deleting notification")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows wrapped, got: %v", err)
	}
}

// TestNotification_SetActive verifies that SetNotificationActive toggles the active flag.
func TestNotification_SetActive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	n := sampleNotification()
	n.Active = true
	if err := s.SaveNotification(ctx, n); err != nil {
		t.Fatalf("SaveNotification: %v", err)
	}

	if err := s.SetNotificationActive(ctx, n.ID, false); err != nil {
		t.Fatalf("SetNotificationActive false: %v", err)
	}
	got, err := s.GetNotification(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if got.Active {
		t.Error("expected Active=false after SetNotificationActive(false)")
	}

	if err := s.SetNotificationActive(ctx, n.ID, true); err != nil {
		t.Fatalf("SetNotificationActive true: %v", err)
	}
	got, err = s.GetNotification(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if !got.Active {
		t.Error("expected Active=true after SetNotificationActive(true)")
	}
}

func strPtr(s string) *string { return &s }

// ensure model.Notification is used
var _ *model.Notification = (*model.Notification)(nil)
