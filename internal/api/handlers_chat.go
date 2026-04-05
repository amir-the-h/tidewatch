package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// handleChatEstimate estimates the token cost for the selected tweets and
// returns whether they fit within the digest model's context window.
//
// POST /api/chat/estimate
// Body: { "tweet_ids": ["..."] }
// Response: { "tokens": int, "limit": int, "ok": bool, "tweet_count": int }
func (s *Server) handleChatEstimate(w http.ResponseWriter, r *http.Request) {
	if s.actionCache == nil {
		writeError(w, http.StatusServiceUnavailable, "action cache not available")
		return
	}
	cp := s.findChatPipeline()
	if cp == nil {
		writeError(w, http.StatusUnprocessableEntity, "no active chat-capable model (requires an Ollama model)")
		return
	}

	var req struct {
		TweetIDs []string `json:"tweet_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.TweetIDs) == 0 {
		writeError(w, http.StatusBadRequest, "tweet_ids required")
		return
	}

	tweets := s.fetchTweetsByIDs(r.Context(), req.TweetIDs)
	formatted := formatTweetsForChat(tweets)

	estimated, limit, err := cp.EstimateTokens(r.Context(), formatted)
	if err != nil {
		s.logger.Error("api: chat estimate: tokens", "error", err)
		writeError(w, http.StatusInternalServerError, "token estimation failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tokens":      estimated,
		"limit":       limit,
		"ok":          estimated < limit,
		"tweet_count": len(tweets),
	})
}

// handleChatStream streams a single chat turn as Server-Sent Events.
// Each event carries a JSON-encoded string chunk; the final event is "[DONE]".
//
// POST /api/chat/stream
// Body: { "tweet_ids": ["..."], "messages": [{"role":"user","content":"..."}] }
// Response: text/event-stream
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if s.actionCache == nil {
		writeError(w, http.StatusServiceUnavailable, "action cache not available")
		return
	}
	cp := s.findChatPipeline()
	if cp == nil {
		writeError(w, http.StatusUnprocessableEntity, "no active chat-capable model")
		return
	}

	var req struct {
		TweetIDs []string                 `json:"tweet_ids"`
		Messages []inference.ChatMessage  `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages required")
		return
	}

	tweets := s.fetchTweetsByIDs(r.Context(), req.TweetIDs)
	formatted := formatTweetsForChat(tweets)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(http.StatusOK)

	// Send an immediate keep-alive so any upstream proxy resets its read
	// timeout before we block on the semaphore or Ollama cold-load.
	flusher, canFlush := w.(http.Flusher)
	fmt.Fprint(w, ": keep-alive\n\n")
	if canFlush {
		flusher.Flush()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	// Periodic keep-alive pings prevent intermediate proxies (whose
	// proxy_read_timeout we cannot control) from issuing a 504 while
	// Ollama is thinking between tokens.
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

	if err := cp.ChatStream(ctx, formatted, req.Messages, w); err != nil {
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

// fetchTweetsByIDs fetches tweets by their IDs, skipping any not found.
func (s *Server) fetchTweetsByIDs(ctx context.Context, ids []string) []*model.Tweet {
	tweets := make([]*model.Tweet, 0, len(ids))
	for _, id := range ids {
		t, err := s.store.GetTweet(ctx, id)
		if err != nil {
			s.logger.Warn("api: chat: tweet not found", "tweet_id", id)
			continue
		}
		tweets = append(tweets, t)
	}
	return tweets
}

// formatTweetsForChat formats tweets as "@handle: text" strings for LLM context.
func formatTweetsForChat(tweets []*model.Tweet) []string {
	out := make([]string, len(tweets))
	for i, t := range tweets {
		if t.AuthorHandle != "" {
			out[i] = fmt.Sprintf("@%s: %s", t.AuthorHandle, t.Text)
		} else {
			out[i] = t.Text
		}
	}
	return out
}
