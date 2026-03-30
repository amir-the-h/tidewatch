package api

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func TestParseCoordinates(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		coordType model.CoordinateType
		want     [][2]float64
		wantErr  bool
	}{
		{
			name:      "array_of_pairs — two points",
			raw:       `[[35.6892,51.389],[32.4279,53.688]]`,
			coordType: model.CoordinateTypeArrayOfPairs,
			want:      [][2]float64{{35.6892, 51.389}, {32.4279, 53.688}},
		},
		{
			name:      "array_of_pairs — empty type defaults to array_of_pairs",
			raw:       `[[35.6892,51.389]]`,
			coordType: "",
			want:      [][2]float64{{35.6892, 51.389}},
		},
		{
			name:      "single_pair",
			raw:       `[35.6892,51.389]`,
			coordType: model.CoordinateTypeSinglePair,
			want:      [][2]float64{{35.6892, 51.389}},
		},
		{
			name:      "object",
			raw:       `{"lat":35.6892,"lon":51.389}`,
			coordType: model.CoordinateTypeObject,
			want:      [][2]float64{{35.6892, 51.389}},
		},
		{
			name:      "array_of_objects — two points",
			raw:       `[{"lat":35.6892,"lon":51.389},{"lat":32.4279,"lon":53.688}]`,
			coordType: model.CoordinateTypeArrayOfObjects,
			want:      [][2]float64{{35.6892, 51.389}, {32.4279, 53.688}},
		},
		{
			name:      "geojson_point — lon/lat order is swapped to lat/lon",
			raw:       `{"type":"Point","coordinates":[51.389,35.6892]}`,
			coordType: model.CoordinateTypeGeoJSONPoint,
			want:      [][2]float64{{35.6892, 51.389}},
		},
		{
			name:      "invalid JSON returns error",
			raw:       `not-json`,
			coordType: model.CoordinateTypeArrayOfPairs,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCoordinates(tt.raw, tt.coordType)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len mismatch: got %d, want %d", len(got), len(tt.want))
			}
			for i, pair := range got {
				if pair[0] != tt.want[i][0] || pair[1] != tt.want[i][1] {
					t.Errorf("pair[%d]: got %v, want %v", i, pair, tt.want[i])
				}
			}
		})
	}
}
