package store

import (
	"context"
	"testing"

	"github.com/amir-the-h/tidewatch/internal/model"
)

func TestOllamaBackends_CRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	b, err := s.CreateOllamaBackend(ctx, "local", "http://localhost:11434", 1)
	if err != nil {
		t.Fatalf("CreateOllamaBackend: %v", err)
	}
	if b.ID == 0 || b.Name != "local" || b.URL != "http://localhost:11434" || !b.Enabled {
		t.Fatalf("unexpected backend: %+v", b)
	}

	all, err := s.ListOllamaBackends(ctx)
	if err != nil {
		t.Fatalf("ListOllamaBackends: %v", err)
	}
	if len(all) != 1 || all[0].ID != b.ID {
		t.Fatalf("expected 1 backend, got %d", len(all))
	}

	got, err := s.GetOllamaBackend(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetOllamaBackend: %v", err)
	}
	if got.Name != "local" {
		t.Fatalf("unexpected name: %s", got.Name)
	}

	if err := s.UpdateOllamaBackend(ctx, b.ID, "remote", "http://remote:11434", 2); err != nil {
		t.Fatalf("UpdateOllamaBackend: %v", err)
	}
	got, _ = s.GetOllamaBackend(ctx, b.ID)
	if got.Name != "remote" || got.URL != "http://remote:11434" {
		t.Fatalf("update not reflected: %+v", got)
	}

	if err := s.SetOllamaBackendEnabled(ctx, b.ID, false); err != nil {
		t.Fatalf("SetOllamaBackendEnabled: %v", err)
	}
	got, _ = s.GetOllamaBackend(ctx, b.ID)
	if got.Enabled {
		t.Fatal("expected disabled")
	}

	if err := s.DeleteOllamaBackend(ctx, b.ID); err != nil {
		t.Fatalf("DeleteOllamaBackend: %v", err)
	}
	all, _ = s.ListOllamaBackends(ctx)
	if len(all) != 0 {
		t.Fatal("expected 0 backends after delete")
	}
}

func TestActionBackends_SetAndList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	act := sampleAction()
	act.Name = "action-for-backends"
	if err := s.SaveAction(ctx, act); err != nil {
		t.Fatalf("SaveAction: %v", err)
	}
	b1, _ := s.CreateOllamaBackend(ctx, "b1", "http://b1:11434", 1)
	b2, _ := s.CreateOllamaBackend(ctx, "b2", "http://b2:11434", 1)

	assignments := []model.ActionBackend{
		{ActionID: act.ID, BackendID: b1.ID, Priority: 1},
		{ActionID: act.ID, BackendID: b2.ID, Priority: 2},
	}
	if err := s.SetActionBackends(ctx, act.ID, assignments); err != nil {
		t.Fatalf("SetActionBackends: %v", err)
	}

	list, err := s.ListActionBackends(ctx, act.ID)
	if err != nil {
		t.Fatalf("ListActionBackends: %v", err)
	}
	if len(list) != 2 || list[0].Priority != 1 || list[1].Priority != 2 {
		t.Fatalf("unexpected list: %+v", list)
	}

	if err := s.SetActionBackends(ctx, act.ID, []model.ActionBackend{
		{ActionID: act.ID, BackendID: b2.ID, Priority: 1},
	}); err != nil {
		t.Fatalf("SetActionBackends replace: %v", err)
	}
	list, _ = s.ListActionBackends(ctx, act.ID)
	if len(list) != 1 || list[0].BackendID != b2.ID {
		t.Fatalf("expected only b2 after replace: %+v", list)
	}

	b3, _ := s.CreateOllamaBackend(ctx, "b3", "http://b3:11434", 1)
	_ = s.SetActionBackends(ctx, act.ID, []model.ActionBackend{
		{ActionID: act.ID, BackendID: b2.ID, Priority: 1},
		{ActionID: act.ID, BackendID: b3.ID, Priority: 2},
	})
	if err := s.RemoveActionBackend(ctx, act.ID, b3.ID); err != nil {
		t.Fatalf("RemoveActionBackend: %v", err)
	}
	list, _ = s.ListActionBackends(ctx, act.ID)
	if len(list) != 1 || list[0].BackendID != b2.ID {
		t.Fatalf("expected only b2 after remove: %+v", list)
	}
}

func seedOllamaBackend(t *testing.T, s Store, name string, enabled bool) int64 {
	t.Helper()
	b, err := s.CreateOllamaBackend(context.Background(), name, "http://"+name+":11434", 1)
	if err != nil {
		t.Fatalf("CreateOllamaBackend(%q): %v", name, err)
	}
	if !enabled {
		if err := s.SetOllamaBackendEnabled(context.Background(), b.ID, false); err != nil {
			t.Fatalf("SetOllamaBackendEnabled: %v", err)
		}
	}
	return b.ID
}

func TestBulkDeleteOllamaBackends_All(t *testing.T) {
	s := newTestStore(t)
	seedOllamaBackend(t, s, "backend-1", true)
	seedOllamaBackend(t, s, "backend-2", false)

	n, err := s.BulkDeleteOllamaBackends(context.Background(), BulkDeleteOllamaBackendsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteOllamaBackends: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	backends, _ := s.ListOllamaBackends(context.Background())
	if len(backends) != 0 {
		t.Errorf("expected 0 backends remaining, got %d", len(backends))
	}
}

func TestBulkDeleteOllamaBackends_ByIDs(t *testing.T) {
	s := newTestStore(t)
	id1 := seedOllamaBackend(t, s, "backend-1", true)
	id2 := seedOllamaBackend(t, s, "backend-2", true)

	n, err := s.BulkDeleteOllamaBackends(context.Background(), BulkDeleteOllamaBackendsFilter{IDs: []int64{id1}})
	if err != nil {
		t.Fatalf("BulkDeleteOllamaBackends: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	backends, _ := s.ListOllamaBackends(context.Background())
	if len(backends) != 1 || backends[0].ID != id2 {
		t.Errorf("expected only backend-2 remaining")
	}
}

func TestBulkDeleteOllamaBackends_ByEnabled(t *testing.T) {
	s := newTestStore(t)
	id1 := seedOllamaBackend(t, s, "enabled-backend", true)
	id2 := seedOllamaBackend(t, s, "disabled-backend", false)

	enabledFalse := false
	n, err := s.BulkDeleteOllamaBackends(context.Background(), BulkDeleteOllamaBackendsFilter{Enabled: &enabledFalse})
	if err != nil {
		t.Fatalf("BulkDeleteOllamaBackends: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	backends, _ := s.ListOllamaBackends(context.Background())
	if len(backends) != 1 || backends[0].ID != id1 {
		t.Errorf("expected only enabled backend remaining, id2=%d", id2)
	}
}

func TestListAllActionBackendIDs(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	act := sampleAction()
	act.Name = "action-for-all-backends"
	_ = s.SaveAction(ctx, act)
	b1, _ := s.CreateOllamaBackend(ctx, "b1", "http://b1:11434", 1)
	b2, _ := s.CreateOllamaBackend(ctx, "b2", "http://b2:11434", 1)
	_ = s.SetActionBackends(ctx, act.ID, []model.ActionBackend{
		{ActionID: act.ID, BackendID: b1.ID, Priority: 1},
		{ActionID: act.ID, BackendID: b2.ID, Priority: 2},
	})

	m, err := s.ListAllActionBackendIDs(ctx)
	if err != nil {
		t.Fatalf("ListAllActionBackendIDs: %v", err)
	}
	ids, ok := m[act.ID]
	if !ok || len(ids) != 2 || ids[0] != b1.ID || ids[1] != b2.ID {
		t.Fatalf("unexpected map: %+v", m)
	}
}
