package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/amir-the-h/tidewatch/internal/model"
)

const defaultMapPageSize = 50

type mapPinResponse struct {
	SignalID          int64             `json:"signal_id"`
	SignalPublishedAt string            `json:"signal_published_at"`
	SignalText        string            `json:"signal_text,omitempty"`
	ModelName         string            `json:"model_name"`
	Coordinates       [][2]float64      `json:"coordinates"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	PopupTemplate     string            `json:"popup_template,omitempty"`
}

// decodeMetadata parses the JSON object produced by json_group_object into a
// string map. Each value in action_execution_metadata is itself JSON-encoded;
// json(value) in the SQL unwraps one level, so string values arrive as JSON
// strings and are decoded to plain strings here. Non-string values (arrays,
// numbers) are kept as their compact JSON representation.
func decodeMetadata(raw string) map[string]string {
	if raw == "" || raw == "null" || raw == "{}" {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		var s string
		if json.Unmarshal(v, &s) == nil {
			out[k] = s
		} else {
			out[k] = string(v)
		}
	}
	return out
}

func (s *Server) handleListMapPins(w http.ResponseWriter, r *http.Request) {
	limit := defaultMapPageSize
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	pins, err := s.store.ListMapPins(r.Context(), limit, offset)
	if err != nil {
		s.logger.Error("api: list map pins", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load map pins")
		return
	}

	out := make([]mapPinResponse, 0, len(pins))
	for _, pin := range pins {
		coords, err := model.ParseCoordinates(pin.CoordinatesJSON, pin.CoordinateType)
		if err != nil || len(coords) == 0 {
			continue
		}

		out = append(out, mapPinResponse{
			SignalID:          pin.SignalID,
			SignalPublishedAt: pin.SignalPublishedAt,
			SignalText:        pin.SignalText,
			ModelName:         pin.ModelName,
			Coordinates:       coords,
			Metadata:          decodeMetadata(pin.MetadataJSON),
			PopupTemplate:     pin.PopupTemplate,
		})
	}

	writeJSON(w, http.StatusOK, out)
}
