package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/worker"
)

type stubPoolStats struct {
	stats worker.PoolStats
}

func (s *stubPoolStats) Stats() worker.PoolStats { return s.stats }

type stubSizerQueue struct{ pending int64 }

func (s *stubSizerQueue) Len(_ context.Context, _ string) (int64, error)    { return s.pending, nil }
func (s *stubSizerQueue) TotalTaskLen(_ context.Context) (int64, error)     { return s.pending, nil }

func TestHandleGetQueue_Shape(t *testing.T) {
	srv := &Server{sizer: &stubSizerQueue{pending: 5}, pool: &stubPoolStats{}}
	req := httptest.NewRequest(http.MethodGet, "/api/queue", nil)
	rr := httptest.NewRecorder()
	srv.handleGetQueue(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["pending_tasks"]; !ok {
		t.Error("response missing 'pending_tasks'")
	}
	if _, ok := body["pending_ollama"]; !ok {
		t.Error("response missing 'pending_ollama'")
	}
	if _, ok := body["workers"]; !ok {
		t.Error("response missing 'workers'")
	}
	if _, ok := body["pending"]; ok {
		t.Error("response should not contain legacy 'pending' key")
	}
	if _, ok := body["queues"]; ok {
		t.Error("response should not contain 'queues'")
	}
}
