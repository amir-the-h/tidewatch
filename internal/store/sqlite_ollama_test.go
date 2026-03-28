package store

import (
	"context"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func TestOllamaBackends_CRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	b, err := s.CreateOllamaBackend(ctx, "local", "http://localhost:11434")
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

	if err := s.UpdateOllamaBackend(ctx, b.ID, "remote", "http://remote:11434"); err != nil {
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

func TestPipelineBackends_SetAndList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	pipe := samplePipeline()
	if err := s.SavePipeline(ctx, pipe); err != nil {
		t.Fatalf("SavePipeline: %v", err)
	}
	b1, _ := s.CreateOllamaBackend(ctx, "b1", "http://b1:11434")
	b2, _ := s.CreateOllamaBackend(ctx, "b2", "http://b2:11434")

	assignments := []model.PipelineBackend{
		{PipelineID: pipe.ID, BackendID: b1.ID, Priority: 1},
		{PipelineID: pipe.ID, BackendID: b2.ID, Priority: 2},
	}
	if err := s.SetPipelineBackends(ctx, pipe.ID, assignments); err != nil {
		t.Fatalf("SetPipelineBackends: %v", err)
	}

	list, err := s.ListPipelineBackends(ctx, pipe.ID)
	if err != nil {
		t.Fatalf("ListPipelineBackends: %v", err)
	}
	if len(list) != 2 || list[0].Priority != 1 || list[1].Priority != 2 {
		t.Fatalf("unexpected list: %+v", list)
	}

	if err := s.SetPipelineBackends(ctx, pipe.ID, []model.PipelineBackend{
		{PipelineID: pipe.ID, BackendID: b2.ID, Priority: 1},
	}); err != nil {
		t.Fatalf("SetPipelineBackends replace: %v", err)
	}
	list, _ = s.ListPipelineBackends(ctx, pipe.ID)
	if len(list) != 1 || list[0].BackendID != b2.ID {
		t.Fatalf("expected only b2 after replace: %+v", list)
	}

	b3, _ := s.CreateOllamaBackend(ctx, "b3", "http://b3:11434")
	_ = s.SetPipelineBackends(ctx, pipe.ID, []model.PipelineBackend{
		{PipelineID: pipe.ID, BackendID: b2.ID, Priority: 1},
		{PipelineID: pipe.ID, BackendID: b3.ID, Priority: 2},
	})
	if err := s.RemovePipelineBackend(ctx, pipe.ID, b3.ID); err != nil {
		t.Fatalf("RemovePipelineBackend: %v", err)
	}
	list, _ = s.ListPipelineBackends(ctx, pipe.ID)
	if len(list) != 1 || list[0].BackendID != b2.ID {
		t.Fatalf("expected only b2 after remove: %+v", list)
	}
}

func TestListAllPipelineBackendIDs(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	pipe := samplePipeline()
	_ = s.SavePipeline(ctx, pipe)
	b1, _ := s.CreateOllamaBackend(ctx, "b1", "http://b1:11434")
	b2, _ := s.CreateOllamaBackend(ctx, "b2", "http://b2:11434")
	_ = s.SetPipelineBackends(ctx, pipe.ID, []model.PipelineBackend{
		{PipelineID: pipe.ID, BackendID: b1.ID, Priority: 1},
		{PipelineID: pipe.ID, BackendID: b2.ID, Priority: 2},
	})

	m, err := s.ListAllPipelineBackendIDs(ctx)
	if err != nil {
		t.Fatalf("ListAllPipelineBackendIDs: %v", err)
	}
	ids, ok := m[pipe.ID]
	if !ok || len(ids) != 2 || ids[0] != b1.ID || ids[1] != b2.ID {
		t.Fatalf("unexpected map: %+v", m)
	}
}
