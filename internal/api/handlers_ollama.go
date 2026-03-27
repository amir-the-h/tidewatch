package api

import (
	"encoding/json"
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/inference"
)

func (s *Server) handleOllamaBackends(w http.ResponseWriter, r *http.Request) {
	// Collect stats from all pipelines that manage a backend pool.
	type merged struct {
		stat        inference.BackendStat
		scoreSum    float64
		scoreCount  int
		allHealthy  bool
		initialized bool
	}
	byURL := make(map[string]*merged)

	for _, p := range s.registry.All() {
		bs, ok := p.(inference.BackendStatter)
		if !ok {
			continue
		}
		for _, stat := range bs.BackendStats() {
			m, exists := byURL[stat.URL]
			if !exists {
				m = &merged{allHealthy: true}
				byURL[stat.URL] = m
			}
			if !m.initialized {
				m.stat.URL = stat.URL
				m.stat.Name = stat.Name
				m.initialized = true
			}
			// healthy: true only if every pipeline instance is healthy
			if !stat.Healthy {
				m.allHealthy = false
			}
			// in_use: true if any pipeline instance is in use
			if stat.InUse {
				m.stat.InUse = true
			}
			// score: accumulate for averaging (skip zero — not yet measured)
			if stat.Score > 0 {
				m.scoreSum += stat.Score
				m.scoreCount++
			}
			// request_count: sum
			m.stat.RequestCount += stat.RequestCount
			// last_active_at: keep most recent
			if stat.LastActiveAt != nil {
				if m.stat.LastActiveAt == nil || stat.LastActiveAt.After(*m.stat.LastActiveAt) {
					t := *stat.LastActiveAt
					m.stat.LastActiveAt = &t
				}
			}
		}
	}

	// Build result slice ordered by URL for stable output.
	// Collect URLs first then sort for determinism.
	urls := make([]string, 0, len(byURL))
	for u := range byURL {
		urls = append(urls, u)
	}
	// Simple insertion sort — pool sizes are small.
	for i := 1; i < len(urls); i++ {
		for j := i; j > 0 && urls[j] < urls[j-1]; j-- {
			urls[j], urls[j-1] = urls[j-1], urls[j]
		}
	}

	result := make([]inference.BackendStat, 0, len(urls))
	for _, u := range urls {
		m := byURL[u]
		stat := m.stat
		stat.Healthy = m.allHealthy
		if m.scoreCount > 0 {
			stat.Score = m.scoreSum / float64(m.scoreCount)
		}
		result = append(result, stat)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
