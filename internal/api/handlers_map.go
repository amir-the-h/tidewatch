package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

const defaultMapPageSize = 50

type mapPinResponse struct {
	TweetID        string            `json:"tweet_id"`
	TweetCreatedAt string            `json:"tweet_created_at"`
	TweetText      string            `json:"tweet_text,omitempty"`
	ModelName      string            `json:"model_name"`
	Coordinates    [][2]float64      `json:"coordinates"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	PopupTemplate  string            `json:"popup_template,omitempty"`
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

// parseCoordinates normalises a raw JSON coordinate value to [][2]float64{lat, lon}.
// An empty or unknown CoordinateType falls back to array_of_pairs behaviour.
// If the stored value is a JSON string (e.g. "[[lat,lon]]" instead of [[lat,lon]]),
// it is unwrapped before parsing — this handles LLM outputs that embed the array
// as a string rather than a native JSON array.
func parseCoordinates(raw string, t model.CoordinateType) ([][2]float64, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var inner string
		if err := json.Unmarshal([]byte(raw), &inner); err == nil {
			raw = inner
		}
	}
	switch t {
	case model.CoordinateTypeSinglePair:
		var pair [2]float64
		if err := json.Unmarshal([]byte(raw), &pair); err != nil {
			return nil, fmt.Errorf("parse single_pair: %w", err)
		}
		return [][2]float64{pair}, nil

	case model.CoordinateTypeObject:
		var obj struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		}
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			return nil, fmt.Errorf("parse object: %w", err)
		}
		return [][2]float64{{obj.Lat, obj.Lon}}, nil

	case model.CoordinateTypeArrayOfObjects:
		var objs []struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		}
		if err := json.Unmarshal([]byte(raw), &objs); err != nil {
			return nil, fmt.Errorf("parse array_of_objects: %w", err)
		}
		out := make([][2]float64, len(objs))
		for i, o := range objs {
			out[i] = [2]float64{o.Lat, o.Lon}
		}
		return out, nil

	case model.CoordinateTypeGeoJSONPoint:
		var pt struct {
			Coordinates [2]float64 `json:"coordinates"` // GeoJSON: [lon, lat]
		}
		if err := json.Unmarshal([]byte(raw), &pt); err != nil {
			return nil, fmt.Errorf("parse geojson_point: %w", err)
		}
		// Swap lon/lat → lat/lon
		return [][2]float64{{pt.Coordinates[1], pt.Coordinates[0]}}, nil

	default: // array_of_pairs (and empty/unknown type)
		var coords [][2]float64
		if err := json.Unmarshal([]byte(raw), &coords); err != nil {
			return nil, fmt.Errorf("parse array_of_pairs: %w", err)
		}
		return coords, nil
	}
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
		coords, err := parseCoordinates(pin.CoordinatesJSON, pin.CoordinateType)
		if err != nil || len(coords) == 0 {
			continue
		}

		out = append(out, mapPinResponse{
			TweetID:        pin.TweetID,
			TweetCreatedAt: pin.TweetCreatedAt,
			TweetText:      pin.TweetText,
			ModelName:      pin.ModelName,
			Coordinates:    coords,
			Metadata:       decodeMetadata(pin.MetadataJSON),
			PopupTemplate:  pin.PopupTemplate,
		})
	}

	writeJSON(w, http.StatusOK, out)
}
