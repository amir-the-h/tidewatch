package api

import (
	"encoding/json"
	"net/http"
)

type mapPinResponse struct {
	TweetID        string            `json:"tweet_id"`
	TweetCreatedAt string            `json:"tweet_created_at"`
	ModelName      string            `json:"model_name"`
	Description    string            `json:"description,omitempty"`
	Locations      []json.RawMessage `json:"locations"`
}

func (s *Server) handleListMapPins(w http.ResponseWriter, r *http.Request) {
	pins, err := s.store.ListMapPins(r.Context())
	if err != nil {
		s.logger.Error("api: list map pins", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load map pins")
		return
	}

	out := make([]mapPinResponse, 0, len(pins))
	for _, pin := range pins {
		var locs []json.RawMessage
		if err := json.Unmarshal([]byte(pin.LocationsJSON), &locs); err != nil {
			continue // skip rows where locations JSON is malformed
		}
		// Strip entries without coordinates (guard against partial extractor output)
		valid := locs[:0]
		for _, l := range locs {
			var check struct {
				Coordinates []float64 `json:"coordinates"`
			}
			if json.Unmarshal(l, &check) == nil && len(check.Coordinates) == 2 {
				valid = append(valid, l)
			}
		}
		if len(valid) == 0 {
			continue
		}

		desc := pin.Description
		if len(desc) > 0 {
			// description is stored as a JSON-encoded string: `"text"` → strip quotes
			var decoded string
			if json.Unmarshal([]byte(desc), &decoded) == nil {
				desc = decoded
			}
		}

		out = append(out, mapPinResponse{
			TweetID:        pin.TweetID,
			TweetCreatedAt: pin.TweetCreatedAt,
			ModelName:      pin.ModelName,
			Description:    desc,
			Locations:      valid,
		})
	}

	writeJSON(w, http.StatusOK, out)
}
