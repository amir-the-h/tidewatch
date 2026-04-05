package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

// handleChatStream runs the agentic chat loop and streams events as SSE.
//
// POST /api/chat/stream
// Body: { "messages": [{"role":"user","content":"..."}] }
// Response: text/event-stream
//
// Event types:
//
//	{"type":"tool_call","name":"...","args":{...}}
//	{"type":"tool_result","name":"...","result":"..."}
//	{"type":"text_chunk","content":"..."}
//	[DONE]
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if s.actionCache == nil {
		writeError(w, http.StatusServiceUnavailable, "action cache not available")
		return
	}
	cp := s.findChatPipeline()
	if cp == nil {
		writeError(w, http.StatusUnprocessableEntity, "no active chat-capable model (requires an active Ollama action)")
		return
	}
	if s.toolExec == nil {
		writeError(w, http.StatusServiceUnavailable, "tool executor not configured")
		return
	}

	var req struct {
		Messages []inference.ChatMessage `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages required")
		return
	}

	tools := mcpserver.BuildOllamaTools()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	fmt.Fprint(w, ": keep-alive\n\n")
	if canFlush {
		flusher.Flush()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fmt.Fprint(w, ": keep-alive\n\n")
				if canFlush {
					flusher.Flush()
				}
			}
		}
	}()

	if err := cp.AgentStream(ctx, req.Messages, tools, s.toolExec, w); err != nil {
		if !errors.Is(err, context.Canceled) {
			s.logger.Error("api: chat stream", "error", err)
		}
	}
}

// findChatPipeline prefers the pipeline named "digest"; falls back to any ChatPipeline.
// Uses the action cache (active actions only) so deactivated Ollama pipelines are excluded.
func (s *Server) findChatPipeline() inference.ChatPipeline {
	if s.actionCache == nil {
		return nil
	}
	var fallback inference.ChatPipeline
	for _, e := range s.actionCache.All() {
		cp, ok := e.Pipeline.(inference.ChatPipeline)
		if !ok {
			continue
		}
		if e.Pipeline.Name() == "digest" {
			return cp
		}
		if fallback == nil {
			fallback = cp
		}
	}
	return fallback
}
