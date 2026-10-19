package store

import (
	"context"
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

func seedPipeline(t *testing.T, s Store, name string, active bool) *model.Pipeline {
	t.Helper()
	p := &model.Pipeline{
		Name:      name,
		Active:    active,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.SavePipeline(context.Background(), p); err != nil {
		t.Fatalf("SavePipeline(%q): %v", name, err)
	}
	return p
}

func TestBulkDeletePipelines_All(t *testing.T) {
	s := newTestStore(t)
	seedPipeline(t, s, "pipe-1", true)
	seedPipeline(t, s, "pipe-2", false)

	n, err := s.BulkDeletePipelines(context.Background(), BulkDeletePipelinesFilter{})
	if err != nil {
		t.Fatalf("BulkDeletePipelines: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	pipes, _ := s.ListPipelines(context.Background())
	if len(pipes) != 0 {
		t.Errorf("expected 0 pipelines remaining, got %d", len(pipes))
	}
}

func TestBulkDeletePipelines_ByIDs(t *testing.T) {
	s := newTestStore(t)
	p1 := seedPipeline(t, s, "pipe-1", true)
	p2 := seedPipeline(t, s, "pipe-2", true)

	n, err := s.BulkDeletePipelines(context.Background(), BulkDeletePipelinesFilter{IDs: []int64{p1.ID}})
	if err != nil {
		t.Fatalf("BulkDeletePipelines: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	pipes, _ := s.ListPipelines(context.Background())
	if len(pipes) != 1 || pipes[0].ID != p2.ID {
		t.Errorf("expected only p2 remaining")
	}
}

func TestBulkDeletePipelines_ByActive(t *testing.T) {
	s := newTestStore(t)
	p1 := seedPipeline(t, s, "active-pipe", true)
	p2 := seedPipeline(t, s, "inactive-pipe", false)

	activeFalse := false
	n, err := s.BulkDeletePipelines(context.Background(), BulkDeletePipelinesFilter{Active: &activeFalse})
	if err != nil {
		t.Fatalf("BulkDeletePipelines: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	pipes, _ := s.ListPipelines(context.Background())
	if len(pipes) != 1 || pipes[0].ID != p1.ID {
		t.Errorf("expected only active pipeline remaining, p2.ID=%d", p2.ID)
	}
}

func seedTriggerLink(t *testing.T, s Store, triggerType string, pipelineID int64) *model.TriggerPipelineLink {
	t.Helper()
	l := &model.TriggerPipelineLink{
		TriggerType: triggerType,
		PipelineID:  pipelineID,
		Active:      true,
	}
	if err := s.SaveTriggerPipelineLink(context.Background(), l); err != nil {
		t.Fatalf("SaveTriggerPipelineLink: %v", err)
	}
	return l
}

func TestBulkDeleteTriggerPipelineLinks_All(t *testing.T) {
	s := newTestStore(t)
	p := seedPipeline(t, s, "pipe", true)
	seedTriggerLink(t, s, "new_tweet", p.ID)
	seedTriggerLink(t, s, "entity_matched", p.ID)

	n, err := s.BulkDeleteTriggerPipelineLinks(context.Background(), BulkDeleteTriggerPipelineLinksFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteTriggerPipelineLinks: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	links, _ := s.ListTriggerPipelineLinks(context.Background())
	if len(links) != 0 {
		t.Errorf("expected 0 links remaining, got %d", len(links))
	}
}

func TestBulkDeleteTriggerPipelineLinks_ByPipelineID(t *testing.T) {
	s := newTestStore(t)
	p1 := seedPipeline(t, s, "pipe-1", true)
	p2 := seedPipeline(t, s, "pipe-2", true)
	l1 := seedTriggerLink(t, s, "new_tweet", p1.ID)
	l2 := seedTriggerLink(t, s, "new_tweet", p2.ID)

	n, err := s.BulkDeleteTriggerPipelineLinks(context.Background(), BulkDeleteTriggerPipelineLinksFilter{PipelineID: &p1.ID})
	if err != nil {
		t.Fatalf("BulkDeleteTriggerPipelineLinks: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	links, _ := s.ListTriggerPipelineLinks(context.Background())
	if len(links) != 1 || links[0].ID != l2.ID {
		t.Errorf("expected only l2 remaining, l1.ID=%d", l1.ID)
	}
}

func TestBulkDeleteTriggerPipelineLinks_ByTriggerType(t *testing.T) {
	s := newTestStore(t)
	p := seedPipeline(t, s, "pipe", true)
	seedTriggerLink(t, s, "new_tweet", p.ID)
	l2 := seedTriggerLink(t, s, "entity_matched", p.ID)

	n, err := s.BulkDeleteTriggerPipelineLinks(context.Background(), BulkDeleteTriggerPipelineLinksFilter{TriggerType: "new_tweet"})
	if err != nil {
		t.Fatalf("BulkDeleteTriggerPipelineLinks: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	links, _ := s.ListTriggerPipelineLinks(context.Background())
	if len(links) != 1 || links[0].ID != l2.ID {
		t.Errorf("expected only entity_matched link remaining")
	}
}
