package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/amir-the-h/tidewatch/internal/source/x"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/migrations"
)

// newXRulesTestServer returns a server backed by an in-memory store whose
// x_rules routes reach X through rules (nil: the real x_stream connector,
// which finds no active x_stream source).
func newXRulesTestServer(t *testing.T, rules xRuleSyncer) (*Server, store.Store) {
	t.Helper()
	st, err := store.NewSQLiteStore(":memory:", migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if rules == nil {
		rules = x.NewStream(st)
	}
	srv := NewServer("127.0.0.1:0", st, nil, nil, nil, nil, nil, nil, 0, "", slog.New(slog.DiscardHandler), nil, nil, nil, nil, "", nil)
	srv.WithXRules(rules)
	return srv, st
}

func createRule(t *testing.T, srv *Server) int64 {
	t.Helper()
	w, term := doJSON(t, srv, http.MethodPost, "/api/x/terms", `{"name":"iran","type":"keyword","value":"iran"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create term: %d %s", w.Code, w.Body)
	}
	termID := int64(term["id"].(float64))
	w, rule := doJSON(t, srv, http.MethodPost, "/api/x/rules", `{"name":"r","expression":{"src":`+strconv.FormatInt(termID, 10)+`}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create rule: %d %s", w.Code, w.Body)
	}
	return int64(rule["id"].(float64))
}

// Without an active x_stream source a rule is stored active with no X rule
// ID; the stream syncs it when it starts.
func TestXRules_NoActiveStreamOnlyStores(t *testing.T) {
	srv, st := newXRulesTestServer(t, nil)
	id := createRule(t, srv)
	r, err := st.GetXRule(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Active || r.XRuleID != "" || r.CompiledValue == "" {
		t.Errorf("rule after create: active %v x_rule_id %q compiled %q", r.Active, r.XRuleID, r.CompiledValue)
	}
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/x/rules/"+strconv.FormatInt(id, 10)+"/deactivate", ""); w.Code != http.StatusNoContent {
		t.Fatalf("deactivate: %d %s", w.Code, w.Body)
	}
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/x/rules/"+strconv.FormatInt(id, 10)+"/activate", ""); w.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", w.Code, w.Body)
	}
}

// With an active x_stream source, create pushes the rule to X and stores the
// X rule ID; deactivate removes it from X.
func TestXRules_ActiveStreamSyncsWithX(t *testing.T) {
	mr := &mockRuleManager{}
	srv, st := newXRulesTestServer(t, &fakeXRules{api: mr, tier: "pro"})
	id := createRule(t, srv)
	r, _ := st.GetXRule(context.Background(), id)
	if !mr.addRuleCalled || r.XRuleID != "rule_123" {
		t.Errorf("create: AddRule called %v, x_rule_id %q", mr.addRuleCalled, r.XRuleID)
	}
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/x/rules/"+strconv.FormatInt(id, 10)+"/deactivate", ""); w.Code != http.StatusNoContent {
		t.Fatalf("deactivate: %d %s", w.Code, w.Body)
	}
	if !mr.deleteRuleCalled {
		t.Error("deactivate didn't remove the rule from X")
	}
}

// Bulk delete removes the deleted live rules from X too.
func TestXRules_BulkDeleteRemovesFromX(t *testing.T) {
	mr := &mockRuleManager{}
	srv, _ := newXRulesTestServer(t, &fakeXRules{api: mr, tier: "basic"})
	createRule(t, srv)
	mr.deleteRuleCalled = false
	w, out := doJSON(t, srv, http.MethodDelete, "/api/x/rules", `{}`)
	if w.Code != http.StatusOK || out["deleted"] != float64(1) {
		t.Fatalf("bulk delete: %d %s", w.Code, w.Body)
	}
	if !mr.deleteRuleCalled {
		t.Error("bulk delete didn't remove the rule from X")
	}
}
