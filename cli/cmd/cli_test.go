package cmd_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amir-the-h/iran-watcher/cli/cmd"
)

// executeCommand runs a cobra command with the given args and returns stdout, stderr, and any error.
func executeCommand(args ...string) (string, string, error) {
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	err := cmd.ExecuteWithArgs(outBuf, errBuf, args...)
	return outBuf.String(), errBuf.String(), err
}

// TestSourceList tests that `source list` makes GET /api/sources and prints results.
func TestSourceList(t *testing.T) {
	sources := []map[string]interface{}{
		{"id": 1, "type": "account", "value": "@handle", "created_at": "2026-01-01T00:00:00Z"},
		{"id": 2, "type": "keyword", "value": "iran", "created_at": "2026-01-02T00:00:00Z"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/sources" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sources)
	}))
	defer srv.Close()

	out, _, err := executeCommand("--base-url", srv.URL, "source", "list")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(out, "@handle") {
		t.Errorf("expected output to contain @handle, got: %s", out)
	}
	if !strings.Contains(out, "iran") {
		t.Errorf("expected output to contain iran, got: %s", out)
	}
	if !strings.Contains(out, "account") {
		t.Errorf("expected output to contain account, got: %s", out)
	}
	if !strings.Contains(out, "keyword") {
		t.Errorf("expected output to contain keyword, got: %s", out)
	}
}

// TestSourceAdd tests that `source add --type account --value @handle` makes POST /api/sources with correct body.
func TestSourceAdd(t *testing.T) {
	var received map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sources" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("failed to decode body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         3,
			"type":       received["type"],
			"value":      received["value"],
			"created_at": "2026-01-03T00:00:00Z",
		})
	}))
	defer srv.Close()

	out, _, err := executeCommand("--base-url", srv.URL, "source", "add", "--type", "account", "--value", "@handle")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if received["type"] != "account" {
		t.Errorf("expected type=account, got: %s", received["type"])
	}
	if received["value"] != "@handle" {
		t.Errorf("expected value=@handle, got: %s", received["value"])
	}
	if !strings.Contains(out, "added") && !strings.Contains(out, "created") && !strings.Contains(out, "3") {
		t.Errorf("expected success output, got: %s", out)
	}
}

// TestModelActivate tests that `model activate 5` makes POST /api/models/5/activate.
func TestModelActivate(t *testing.T) {
	activated := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/models/5/activate" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		activated = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": 5, "active": true})
	}))
	defer srv.Close()

	_, _, err := executeCommand("--base-url", srv.URL, "model", "activate", "5")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !activated {
		t.Error("expected POST /api/models/5/activate to be called")
	}
}

// TestHealth tests that `health` prints status and model info.
func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/health" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"models": []map[string]interface{}{
				{"name": "sentiment", "active": true},
			},
		})
	}))
	defer srv.Close()

	out, _, err := executeCommand("--base-url", srv.URL, "health")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(out, "ok") {
		t.Errorf("expected output to contain ok, got: %s", out)
	}
}

// TestNon200Response tests that non-200 responses print the error message and return an error.
func TestNon200Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "internal server error"})
	}))
	defer srv.Close()

	_, errOut, err := executeCommand("--base-url", srv.URL, "source", "list")
	if err == nil {
		t.Error("expected error for non-200 response")
	}

	combined := errOut
	if !strings.Contains(combined, "internal server error") && !strings.Contains(combined, "error") {
		t.Errorf("expected error message in output, got stderr: %q", errOut)
	}
}
