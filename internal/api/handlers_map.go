package api

import (
	"encoding/json"
	"net/http"
)

type mapPinResponse struct {
	TweetID        string        `json:"tweet_id"`
	TweetCreatedAt string        `json:"tweet_created_at"`
	ModelName      string        `json:"model_name"`
	Description    string        `json:"description,omitempty"`
	Coordinates    [][2]float64  `json:"coordinates"`
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
		var coords [][2]float64
		if err := json.Unmarshal([]byte(pin.CoordinatesJSON), &coords); err != nil {
			continue
		}
		if len(coords) == 0 {
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
			Coordinates:    coords,
		})
	}

	writeJSON(w, http.StatusOK, out)
}
