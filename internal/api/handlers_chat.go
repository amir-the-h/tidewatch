package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

// lockedWriter wraps an http.ResponseWriter with a mutex so that the
// keep-alive goroutine and AgentStream can safely write concurrently.
type lockedWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
}

func (lw *lockedWriter) Write(b []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(b)
}

func (lw *lockedWriter) Flush() {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if f, ok := lw.w.(http.Flusher); ok {
		f.Flush()
	}
}

// handleChatTools returns all available MCP tool names and descriptions.
//
// GET /api/chat/tools
// Response: [{"name":"...","description":"..."}]
func (s *Server) handleChatTools(w http.ResponseWriter, r *http.Request) {
	all := mcpserver.AllTools()
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	out := make([]toolInfo, len(all))
	for i, td := range all {
		out[i] = toolInfo{Name: td.Name, Description: td.Description}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleChatStream runs the agentic chat loop and streams events as SSE.
//
// POST /api/chat/stream
// Body: { "action_id": "123", "messages": [{"role":"user","content":"..."}] }
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
	if s.toolExec == nil {
		writeError(w, http.StatusServiceUnavailable, "tool executor not configured")
		return
	}

	// Decode and validate request before committing to SSE response.
	var req struct {
		ActionID string                  `json:"action_id"`
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

	var actionID int64
	if req.ActionID != "" {
		var err error
		actionID, err = strconv.ParseInt(req.ActionID, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid action_id")
			return
		}
	}

	cp, allowedTools := s.findChatPipeline(actionID)
	if cp == nil {
		writeError(w, http.StatusUnprocessableEntity, "no active chat-capable model (requires an active Ollama action)")
		return
	}

	tools := mcpserver.BuildOllamaTools(allowedTools)

	// Commit to SSE — no more early returns after this point.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Wrap w so keep-alive goroutine and AgentStream can write concurrently.
	lw := &lockedWriter{w: w}
	fmt.Fprint(lw, ": keep-alive\n\n")
	lw.Flush()

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
				fmt.Fprint(lw, ": keep-alive\n\n")
				lw.Flush()
			}
		}
	}()

	if err := cp.AgentStream(ctx, req.Messages, tools, s.toolExec, lw); err != nil {
		if !errors.Is(err, context.Canceled) {
			s.logger.Error("api: chat stream", "error", err)
		}
	}
}

// findChatPipeline returns the chat pipeline and its allowed tool list for the
// given action ID. If actionID is 0, falls back to the first active action with
// usecase=="chat", then any active ChatPipeline.
// Returns (nil, nil) if no suitable action is found.
func (s *Server) findChatPipeline(actionID int64) (inference.ChatPipeline, []string) {
	if s.actionCache == nil {
		return nil, nil
	}

	// Targeted lookup by action ID.
	if actionID > 0 {
		e := s.actionCache.Get(actionID)
		if e == nil || !e.Action.Active {
			return nil, nil
		}
		cp, ok := e.Pipeline.(inference.ChatPipeline)
		if !ok {
			return nil, nil
		}
		return cp, actionAllowedTools(e.Action.Config)
	}

	// Fallback: find any active chat action.
	var fallback inference.ChatPipeline
	var fallbackTools []string
	for _, e := range s.actionCache.All() {
		cp, ok := e.Pipeline.(inference.ChatPipeline)
		if !ok {
			continue
		}
		if actionUsecase(e.Action.Config) == "chat" {
			return cp, actionAllowedTools(e.Action.Config)
		}
		if fallback == nil {
			fallback = cp
			fallbackTools = actionAllowedTools(e.Action.Config)
		}
	}
	return fallback, fallbackTools
}

// actionUsecase extracts the "usecase" field from an action's JSON config.
func actionUsecase(cfg json.RawMessage) string {
	if len(cfg) == 0 {
		return ""
	}
	var c struct {
		Usecase string `json:"usecase"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		return ""
	}
	return c.Usecase
}

// actionAllowedTools extracts the "allowed_tools" field from an action's JSON config.
// Returns nil if absent (meaning all tools are allowed).
func actionAllowedTools(cfg json.RawMessage) []string {
	if len(cfg) == 0 {
		return nil
	}
	var c struct {
		AllowedTools []string `json:"allowed_tools"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		return nil
	}
	return c.AllowedTools
}
