package api

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/worker"
)

func TestBuildWorkerRefs_MultipleWorkersOnSameAction(t *testing.T) {
	workers := []worker.WorkerInfo{
		{ID: 1, Action: "Detect Assassination", Pipeline: "ollama", Backend: "http://localhost:11434"},
		{ID: 4, Action: "Detect Assassination", Pipeline: "ollama", Backend: "http://localhost:11435"},
		{ID: 2, Action: "Summarize", Pipeline: "ollama", Backend: "http://localhost:11434"},
	}
	got := buildWorkerRefs(workers)

	if len(got["Detect Assassination"]) != 2 {
		t.Errorf("Detect Assassination: want 2 workers, got %d", len(got["Detect Assassination"]))
	}
	if got["Detect Assassination"][0].ID != 1 {
		t.Errorf("first worker ID: want 1, got %d", got["Detect Assassination"][0].ID)
	}
	if got["Detect Assassination"][1].ID != 4 {
		t.Errorf("second worker ID: want 4, got %d", got["Detect Assassination"][1].ID)
	}
	if len(got["Summarize"]) != 1 {
		t.Errorf("Summarize: want 1 worker, got %d", len(got["Summarize"]))
	}
	if got["Summarize"][0].Backend != "http://localhost:11434" {
		t.Errorf("Summarize backend: want %q, got %q", "http://localhost:11434", got["Summarize"][0].Backend)
	}
}

func TestBuildWorkerRefs_EmptyInput(t *testing.T) {
	got := buildWorkerRefs(nil)
	if len(got) != 0 {
		t.Errorf("want empty map, got %d entries", len(got))
	}
}
