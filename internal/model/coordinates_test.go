package model

import (
	"testing"
)

func TestParseCoordinates_ArrayOfPairs(t *testing.T) {
	pairs, err := ParseCoordinates("[[35.6892,51.389],[32.0,44.0]]", CoordinateTypeArrayOfPairs)
	if err != nil {
		t.Fatalf("ParseCoordinates: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("want 2 pairs, got %d", len(pairs))
	}
	if pairs[0] != [2]float64{35.6892, 51.389} {
		t.Errorf("pair[0] wrong: %v", pairs[0])
	}
	if pairs[1] != [2]float64{32.0, 44.0} {
		t.Errorf("pair[1] wrong: %v", pairs[1])
	}
}

func TestParseCoordinates_SinglePair(t *testing.T) {
	pairs, err := ParseCoordinates("[35.6892,51.389]", CoordinateTypeSinglePair)
	if err != nil {
		t.Fatalf("ParseCoordinates: %v", err)
	}
	if len(pairs) != 1 || pairs[0] != [2]float64{35.6892, 51.389} {
		t.Errorf("unexpected result: %v", pairs)
	}
}

func TestParseCoordinates_Object(t *testing.T) {
	pairs, err := ParseCoordinates(`{"lat":35.6892,"lon":51.389}`, CoordinateTypeObject)
	if err != nil {
		t.Fatalf("ParseCoordinates: %v", err)
	}
	if len(pairs) != 1 || pairs[0] != [2]float64{35.6892, 51.389} {
		t.Errorf("unexpected result: %v", pairs)
	}
}

func TestParseCoordinates_ArrayOfObjects(t *testing.T) {
	pairs, err := ParseCoordinates(`[{"lat":35.6892,"lon":51.389},{"lat":32.0,"lon":44.0}]`, CoordinateTypeArrayOfObjects)
	if err != nil {
		t.Fatalf("ParseCoordinates: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("want 2, got %d", len(pairs))
	}
	if pairs[0] != [2]float64{35.6892, 51.389} || pairs[1] != [2]float64{32.0, 44.0} {
		t.Errorf("wrong pairs: %v", pairs)
	}
}

func TestParseCoordinates_GeoJSONPoint(t *testing.T) {
	// GeoJSON coordinates are [lon, lat] — must be swapped on parse.
	pairs, err := ParseCoordinates(`{"coordinates":[51.389,35.6892]}`, CoordinateTypeGeoJSONPoint)
	if err != nil {
		t.Fatalf("ParseCoordinates: %v", err)
	}
	if len(pairs) != 1 || pairs[0] != [2]float64{35.6892, 51.389} {
		t.Errorf("unexpected result: %v", pairs)
	}
}

func TestParseCoordinates_StringWrapped(t *testing.T) {
	// LLM pipelines sometimes embed the array as a JSON string.
	pairs, err := ParseCoordinates(`"[[35.6892,51.389]]"`, CoordinateTypeArrayOfPairs)
	if err != nil {
		t.Fatalf("ParseCoordinates: %v", err)
	}
	if len(pairs) != 1 || pairs[0] != [2]float64{35.6892, 51.389} {
		t.Errorf("unexpected result: %v", pairs)
	}
}

func TestParseCoordinates_DefaultFallback(t *testing.T) {
	// Empty/unknown coordinate type falls back to array_of_pairs.
	pairs, err := ParseCoordinates("[[1.0,2.0]]", "")
	if err != nil {
		t.Fatalf("ParseCoordinates: %v", err)
	}
	if len(pairs) != 1 || pairs[0] != [2]float64{1.0, 2.0} {
		t.Errorf("unexpected result: %v", pairs)
	}
}
