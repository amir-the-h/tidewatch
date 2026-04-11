package model

import (
	"encoding/json"
	"fmt"
)

// ParseCoordinates normalises a raw JSON coordinate value to [][2]float64{lat, lon}.
// An empty or unknown CoordinateType falls back to array_of_pairs behaviour.
// If the stored value is a JSON string (e.g. "[[lat,lon]]" instead of [[lat,lon]]),
// it is unwrapped before parsing — this handles LLM outputs that embed the array
// as a string rather than a native JSON array.
func ParseCoordinates(raw string, t CoordinateType) ([][2]float64, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var inner string
		if err := json.Unmarshal([]byte(raw), &inner); err == nil {
			raw = inner
		}
	}
	switch t {
	case CoordinateTypeSinglePair:
		var pair [2]float64
		if err := json.Unmarshal([]byte(raw), &pair); err != nil {
			return nil, fmt.Errorf("parse single_pair: %w", err)
		}
		return [][2]float64{pair}, nil

	case CoordinateTypeObject:
		var obj struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		}
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			return nil, fmt.Errorf("parse object: %w", err)
		}
		return [][2]float64{{obj.Lat, obj.Lon}}, nil

	case CoordinateTypeArrayOfObjects:
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

	case CoordinateTypeGeoJSONPoint:
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
