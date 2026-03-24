package api

import (
	"net/http"
)

func (s *Server) handleStreamStatus(w http.ResponseWriter, r *http.Request) {
	connected := s.stream != nil && s.stream.Connected()
	writeJSON(w, http.StatusOK, map[string]any{
		"connected": connected,
	})
}

func (s *Server) handleStreamConnect(w http.ResponseWriter, r *http.Request) {
	if s.stream == nil {
		writeError(w, http.StatusInternalServerError, "stream client not configured")
		return
	}

	if err := s.stream.Connect(s.ctx); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"stream": "connected",
	})
}

func (s *Server) handleStreamDisconnect(w http.ResponseWriter, r *http.Request) {
	if s.stream == nil {
		writeError(w, http.StatusInternalServerError, "stream client not configured")
		return
	}

	s.stream.Disconnect()

	writeJSON(w, http.StatusOK, map[string]any{
		"stream": "disconnected",
	})
}
