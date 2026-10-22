package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/migrations"
)

// The source_* keys come from the event's source, or — for events without
// one (reprocess, pipeline-run backfills) — from the first source that saw
// the signal.
func TestLoadSignalContext_SourceKeys(t *testing.T) {
	st, err := store.NewSQLiteStore(":memory:", migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	tier := 2
	first := &model.Source{Name: "BBC World", Type: "rss", Tier: &tier, Active: true}
	second := &model.Source{Name: "Other feed", Type: "rss", Active: true}
	for _, src := range []*model.Source{first, second} {
		if err := st.SaveSource(ctx, src); err != nil {
			t.Fatal(err)
		}
	}
	sig := &model.Signal{Kind: model.SignalKindArticle, ExtNS: "rss", ExternalID: "g1", Title: "t", PublishedAt: time.Now()}
	if _, err := st.SaveSignal(ctx, sig); err != nil {
		t.Fatal(err)
	}
	_ = st.AddSignalSource(ctx, sig.ID, first.ID)
	_ = st.AddSignalSource(ctx, sig.ID, second.ID)

	load := func(ev queue.SignalEvent) map[string]any {
		payload, _ := json.Marshal(ev)
		got, _ := loadSignalContext(ctx, st, payload)
		return got
	}

	got := load(queue.SignalEvent{SignalID: sig.ID, Kind: "article", SourceID: second.ID, SourceType: "rss"})
	if got["source_id"] != second.ID || got["source_name"] != "Other feed" || got["source_type"] != "rss" {
		t.Errorf("event source: %v", got)
	}
	if _, ok := got["source_tier"]; ok {
		t.Errorf("source_tier set for an unrated source: %v", got["source_tier"])
	}

	got = load(queue.SignalEvent{SignalID: sig.ID, Kind: "article"})
	if got["source_id"] != first.ID || got["source_name"] != "BBC World" || got["source_tier"] != 2 {
		t.Errorf("fallback source: %v", got)
	}

	// The X shim reports a source type without a source row.
	other := &model.Signal{Kind: model.SignalKindPost, ExtNS: "x", ExternalID: "1", PublishedAt: time.Now()}
	_, _ = st.SaveSignal(ctx, other)
	got = load(queue.SignalEvent{SignalID: other.ID, Kind: "post", SourceType: "x_stream"})
	if got["source_type"] != "x_stream" {
		t.Errorf("source_type from event: %v", got)
	}
	if _, ok := got["source_id"]; ok {
		t.Errorf("source_id without a source: %v", got["source_id"])
	}
}
