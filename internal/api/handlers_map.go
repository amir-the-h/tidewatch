package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

type mapPinResponse struct {
	TweetID        string       `json:"tweet_id"`
	TweetCreatedAt string       `json:"tweet_created_at"`
	ModelName      string       `json:"model_name"`
	Description    string       `json:"description,omitempty"`
	Coordinates    [][2]float64 `json:"coordinates"`
}

// parseCoordinates normalises a raw JSON coordinate value to [][2]float64{lat, lon}.
// An empty or unknown CoordinateType falls back to array_of_pairs behaviour.
func parseCoordinates(raw string, t model.CoordinateType) ([][2]float64, error) {
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
	pins, err := s.store.ListMapPins(r.Context())
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

		desc := pin.Description
		if len(desc) > 0 {
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
