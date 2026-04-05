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

func sampleProvider() *model.NotificationProvider {
	return &model.NotificationProvider{
		Name:      "my-pushover",
		Type:      "pushover",
		Config:    json.RawMessage(`{"app_token":"tok","user_key":"key"}`),
		Active:    true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func sampleChannel(providerID int64) *model.NotificationChannel {
	return &model.NotificationChannel{
		Name:      "alert-channel",
		ProviderID: providerID,
		Config:    json.RawMessage(`{"chat_id":"-1001234567890"}`),
		Active:    true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// --- Provider tests ---

func TestNotificationProvider_SaveGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("SaveNotificationProvider: %v", err)
	}
	if p.ID == 0 {
		t.Fatal("expected non-zero ID after save")
	}

	got, err := s.GetNotificationProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetNotificationProvider: %v", err)
	}
	if got.Name != p.Name {
		t.Errorf("Name: got %q, want %q", got.Name, p.Name)
	}
	if got.Type != "pushover" {
		t.Errorf("Type: got %q, want pushover", got.Type)
	}
	if string(got.Config) != string(p.Config) {
		t.Errorf("Config: got %s, want %s", got.Config, p.Config)
	}
	if !got.Active {
		t.Error("expected Active=true")
	}
}

func TestNotificationProvider_GetNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.GetNotificationProvider(ctx, 999)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got: %v", err)
	}
}

func TestNotificationProvider_List(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p1 := sampleProvider()
	p1.Active = true
	if err := s.SaveNotificationProvider(ctx, p1); err != nil {
		t.Fatalf("save p1: %v", err)
	}
	p2 := sampleProvider()
	p2.Name = "my-telegram"
	p2.Type = "telegram"
	p2.Active = false
	if err := s.SaveNotificationProvider(ctx, p2); err != nil {
		t.Fatalf("save p2: %v", err)
	}

	all, err := s.ListNotificationProviders(ctx)
	if err != nil {
		t.Fatalf("ListNotificationProviders: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 providers, got %d", len(all))
	}
}

func TestNotificationProvider_Update(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("save: %v", err)
	}

	p.Name = "updated"
	p.Config = json.RawMessage(`{"app_token":"new","user_key":"new"}`)
	p.UpdatedAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := s.UpdateNotificationProvider(ctx, p); err != nil {
		t.Fatalf("UpdateNotificationProvider: %v", err)
	}

	got, _ := s.GetNotificationProvider(ctx, p.ID)
	if got.Name != "updated" {
		t.Errorf("Name: got %q, want updated", got.Name)
	}
}

func TestNotificationProvider_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.DeleteNotificationProvider(ctx, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := s.GetNotificationProvider(ctx, p.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows after delete, got: %v", err)
	}
}

func TestNotificationProvider_SetActive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	p.Active = true
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.SetNotificationProviderActive(ctx, p.ID, false); err != nil {
		t.Fatalf("SetNotificationProviderActive: %v", err)
	}
	got, _ := s.GetNotificationProvider(ctx, p.ID)
	if got.Active {
		t.Error("expected Active=false")
	}
}

// --- Channel tests ---

func TestNotificationChannel_SaveGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("save provider: %v", err)
	}

	ch := sampleChannel(p.ID)
	if err := s.SaveNotificationChannel(ctx, ch); err != nil {
		t.Fatalf("SaveNotificationChannel: %v", err)
	}
	if ch.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := s.GetNotificationChannel(ctx, ch.ID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	if got.Name != ch.Name {
		t.Errorf("Name: got %q, want %q", got.Name, ch.Name)
	}
	if got.ProviderID != p.ID {
		t.Errorf("ProviderID: got %d, want %d", got.ProviderID, p.ID)
	}
}

func TestNotificationChannel_GetNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.GetNotificationChannel(ctx, 999)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got: %v", err)
	}
}

func TestNotificationChannel_ListByProvider(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p1 := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p1); err != nil {
		t.Fatalf("save p1: %v", err)
	}
	p2 := sampleProvider()
	p2.Name = "p2"
	if err := s.SaveNotificationProvider(ctx, p2); err != nil {
		t.Fatalf("save p2: %v", err)
	}

	ch1 := sampleChannel(p1.ID)
	if err := s.SaveNotificationChannel(ctx, ch1); err != nil {
		t.Fatalf("save ch1: %v", err)
	}
	ch2 := sampleChannel(p2.ID)
	ch2.Name = "ch2"
	if err := s.SaveNotificationChannel(ctx, ch2); err != nil {
		t.Fatalf("save ch2: %v", err)
	}

	// Filter by provider
	results, err := s.ListNotificationChannels(ctx, &p1.ID)
	if err != nil {
		t.Fatalf("ListNotificationChannels: %v", err)
	}
	if len(results) != 1 || results[0].ID != ch1.ID {
		t.Errorf("expected 1 channel for p1, got %d", len(results))
	}

	// All channels
	all, err := s.ListNotificationChannels(ctx, nil)
	if err != nil {
		t.Fatalf("ListNotificationChannels all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 channels total, got %d", len(all))
	}
}

func TestNotificationChannel_Update(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	_ = s.SaveNotificationProvider(ctx, p)
	ch := sampleChannel(p.ID)
	_ = s.SaveNotificationChannel(ctx, ch)

	ch.Config = json.RawMessage(`{"chat_id":"-9999"}`)
	if err := s.UpdateNotificationChannel(ctx, ch); err != nil {
		t.Fatalf("UpdateNotificationChannel: %v", err)
	}
	got, _ := s.GetNotificationChannel(ctx, ch.ID)
	if string(got.Config) != `{"chat_id":"-9999"}` {
		t.Errorf("Config: got %s", got.Config)
	}
}

func TestNotificationChannel_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	_ = s.SaveNotificationProvider(ctx, p)
	ch := sampleChannel(p.ID)
	_ = s.SaveNotificationChannel(ctx, ch)

	if err := s.DeleteNotificationChannel(ctx, ch.ID); err != nil {
		t.Fatalf("DeleteNotificationChannel: %v", err)
	}
	_, err := s.GetNotificationChannel(ctx, ch.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows after delete, got: %v", err)
	}
}

func TestNotificationChannel_SetActive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	_ = s.SaveNotificationProvider(ctx, p)
	ch := sampleChannel(p.ID)
	ch.Active = true
	_ = s.SaveNotificationChannel(ctx, ch)

	if err := s.SetNotificationChannelActive(ctx, ch.ID, false); err != nil {
		t.Fatalf("SetNotificationChannelActive: %v", err)
	}
	got, _ := s.GetNotificationChannel(ctx, ch.ID)
	if got.Active {
		t.Error("expected Active=false")
	}
}

func TestNotificationChannel_DeleteProvider_CascadesChannel(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := sampleProvider()
	_ = s.SaveNotificationProvider(ctx, p)
	ch := sampleChannel(p.ID)
	_ = s.SaveNotificationChannel(ctx, ch)

	_ = s.DeleteNotificationProvider(ctx, p.ID)

	_, err := s.GetNotificationChannel(ctx, ch.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected channel to be cascade-deleted, got: %v", err)
	}
}

// ── BulkDeleteNotificationProviders ─────────────────────────────────────────

func TestBulkDeleteNotificationProviders_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p1 := sampleProvider()
	p1.Name = "prov-1"
	if err := s.SaveNotificationProvider(ctx, p1); err != nil {
		t.Fatalf("SaveNotificationProvider p1: %v", err)
	}
	p2 := sampleProvider()
	p2.Name = "prov-2"
	if err := s.SaveNotificationProvider(ctx, p2); err != nil {
		t.Fatalf("SaveNotificationProvider p2: %v", err)
	}

	n, err := s.BulkDeleteNotificationProviders(ctx, BulkDeleteNotificationProvidersFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationProviders: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	provs, _ := s.ListNotificationProviders(ctx)
	if len(provs) != 0 {
		t.Errorf("expected 0 providers remaining, got %d", len(provs))
	}
}

func TestBulkDeleteNotificationProviders_ByType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p1 := sampleProvider() // type = "pushover"
	p1.Name = "pushover-prov"
	if err := s.SaveNotificationProvider(ctx, p1); err != nil {
		t.Fatalf("SaveNotificationProvider p1: %v", err)
	}
	p2 := sampleProvider()
	p2.Name = "telegram-prov"
	p2.Type = "telegram"
	if err := s.SaveNotificationProvider(ctx, p2); err != nil {
		t.Fatalf("SaveNotificationProvider p2: %v", err)
	}

	n, err := s.BulkDeleteNotificationProviders(ctx, BulkDeleteNotificationProvidersFilter{Type: "pushover"})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationProviders: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	provs, _ := s.ListNotificationProviders(ctx)
	if len(provs) != 1 || provs[0].ID != p2.ID {
		t.Errorf("expected only telegram provider remaining")
	}
}

func TestBulkDeleteNotificationProviders_ByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p1 := sampleProvider()
	p1.Name = "prov-1"
	if err := s.SaveNotificationProvider(ctx, p1); err != nil {
		t.Fatalf("SaveNotificationProvider p1: %v", err)
	}
	p2 := sampleProvider()
	p2.Name = "prov-2"
	if err := s.SaveNotificationProvider(ctx, p2); err != nil {
		t.Fatalf("SaveNotificationProvider p2: %v", err)
	}

	n, err := s.BulkDeleteNotificationProviders(ctx, BulkDeleteNotificationProvidersFilter{IDs: []int64{p1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationProviders: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	provs, _ := s.ListNotificationProviders(ctx)
	if len(provs) != 1 || provs[0].ID != p2.ID {
		t.Errorf("expected only p2 remaining")
	}
}

// ── BulkDeleteNotificationChannels ──────────────────────────────────────────

func TestBulkDeleteNotificationChannels_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()
	if err := s.SaveNotificationProvider(ctx, p); err != nil {
		t.Fatalf("SaveNotificationProvider: %v", err)
	}
	ch1 := sampleChannel(p.ID)
	ch1.Name = "chan-1"
	if err := s.SaveNotificationChannel(ctx, ch1); err != nil {
		t.Fatalf("SaveNotificationChannel ch1: %v", err)
	}
	ch2 := sampleChannel(p.ID)
	ch2.Name = "chan-2"
	if err := s.SaveNotificationChannel(ctx, ch2); err != nil {
		t.Fatalf("SaveNotificationChannel ch2: %v", err)
	}

	n, err := s.BulkDeleteNotificationChannels(ctx, BulkDeleteNotificationChannelsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationChannels: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	chans, _ := s.ListNotificationChannels(ctx, nil)
	if len(chans) != 0 {
		t.Errorf("expected 0 channels remaining, got %d", len(chans))
	}
}

func TestBulkDeleteNotificationChannels_ByProviderID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p1 := sampleProvider()
	p1.Name = "prov-1"
	if err := s.SaveNotificationProvider(ctx, p1); err != nil {
		t.Fatalf("SaveNotificationProvider p1: %v", err)
	}
	p2 := sampleProvider()
	p2.Name = "prov-2"
	if err := s.SaveNotificationProvider(ctx, p2); err != nil {
		t.Fatalf("SaveNotificationProvider p2: %v", err)
	}
	ch1 := sampleChannel(p1.ID)
	if err := s.SaveNotificationChannel(ctx, ch1); err != nil {
		t.Fatalf("SaveNotificationChannel ch1: %v", err)
	}
	ch2 := sampleChannel(p2.ID)
	ch2.Name = "chan-p2"
	if err := s.SaveNotificationChannel(ctx, ch2); err != nil {
		t.Fatalf("SaveNotificationChannel ch2: %v", err)
	}

	n, err := s.BulkDeleteNotificationChannels(ctx, BulkDeleteNotificationChannelsFilter{ProviderID: &p1.ID})
	if err != nil {
		t.Fatalf("BulkDeleteNotificationChannels: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	chans, _ := s.ListNotificationChannels(ctx, nil)
	if len(chans) != 1 || chans[0].ID != ch2.ID {
		t.Errorf("expected only ch2 remaining")
	}
}
