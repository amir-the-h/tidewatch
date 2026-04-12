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
	srv := &Server{
		pool: &stubPoolStats{
			stats: worker.PoolStats{
				Total:  3,
				Active: 1,
				Idle:   2,
				Workers: []worker.WorkerInfo{
					{ID: 1, Status: "active", Action: "NER", Pipeline: "onnx", TweetID: "123", Depth: 0},
					{ID: 2, Status: "idle"},
					{ID: 3, Status: "idle"},
				},
			},
		},
		sizer: &stubSizerQueue{pending: 42},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/queue", nil)
	rr := httptest.NewRecorder()
	srv.handleGetQueue(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := resp["pending"]; !ok {
		t.Error("response missing 'pending' field")
	}
	if _, ok := resp["workers"]; !ok {
		t.Error("response missing 'workers' field")
	}
	if _, ok := resp["queues"]; ok {
		t.Error("response must NOT contain 'queues' field")
	}
}
