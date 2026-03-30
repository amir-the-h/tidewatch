package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

type connectionsStore struct {
	*mockStore
	connections []*model.EntityConnection
}

func (s *connectionsStore) ListEntityConnections(_ context.Context) ([]*model.EntityConnection, error) {
	return s.connections, nil
}

func (s *connectionsStore) DeleteAllEntityConnections(_ context.Context) error {
	s.connections = nil
	return nil
}

func (s *connectionsStore) GetEntityConnectionSubgraph(_ context.Context, entityID int64, _, _ int) ([]*model.EntityConnection, error) {
	var out []*model.EntityConnection
	for _, c := range s.connections {
		if c.FromID == entityID || c.ToID == entityID {
			out = append(out, c)
		}
	}
	if out == nil {
		out = []*model.EntityConnection{}
	}
	return out, nil
}

func TestHandleListEntityConnections(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	cs := &connectionsStore{
		mockStore: ms,
		connections: []*model.EntityConnection{
			{FromID: 1, ToID: 2, Weight: 5, FromName: "Alpha", ToName: "Beta"},
		},
	}
	srv.store = cs

	req := httptest.NewRequest(http.MethodGet, "/api/entity-connections", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Connections []*model.EntityConnection `json:"connections"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Connections) != 1 {
		t.Fatalf("want 1 connection, got %d", len(resp.Connections))
	}
	if resp.Connections[0].Weight != 5 {
		t.Errorf("want weight=5, got %d", resp.Connections[0].Weight)
	}
}

func TestHandleDeleteEntityConnections(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	cs := &connectionsStore{
		mockStore:   ms,
		connections: []*model.EntityConnection{{FromID: 1, ToID: 2, Weight: 3}},
	}
	srv.store = cs

	req := httptest.NewRequest(http.MethodDelete, "/api/entity-connections", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(cs.connections) != 0 {
		t.Errorf("want empty after delete, got %d", len(cs.connections))
	}
}

func TestHandleGetEntityConnectionSubgraph_MissingEntityID(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	srv.store = &connectionsStore{mockStore: ms}

	req := httptest.NewRequest(http.MethodGet, "/api/entity-connections/subgraph", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleGetEntityConnectionSubgraph_ReturnsSubgraph(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	cs := &connectionsStore{
		mockStore: ms,
		connections: []*model.EntityConnection{
			{FromID: 1, ToID: 2, Weight: 3, FromName: "Alpha", ToName: "Beta"},
			{FromID: 2, ToID: 3, Weight: 1, FromName: "Beta", ToName: "Gamma"},
		},
	}
	srv.store = cs

	req := httptest.NewRequest(http.MethodGet, "/api/entity-connections/subgraph?entity_id=1&depth=2&min_weight=1", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Connections []*model.EntityConnection `json:"connections"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Connections) == 0 {
		t.Fatal("want at least one connection")
	}
}
