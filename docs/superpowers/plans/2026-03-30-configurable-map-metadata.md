# Configurable Map Metadata Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow each pipeline to declare, via its `config` JSON, which metadata key holds coordinates and what format they're in, with UI form fields to configure it.

**Architecture:** `CoordinateType` + `MapConfig` types added to `internal/model/pipeline.go`. The `ListMapPins` SQL query uses SQLite `json_extract` to dynamically filter by each pipeline's configured `coordinate_key`. A `parseCoordinates` function in the map handler normalises all 5 coordinate formats to `[][2]float64`. The frontend pipeline form gains two optional fields under a "Map Output" section.

**Tech Stack:** Go (stdlib `encoding/json`), SQLite `json_extract`, Vue 3 + Naive UI (`NInput`, `NSelect`, `NDivider`)

---

### Task 1: Add `MapConfig` types to `internal/model/pipeline.go`

**Files:**
- Modify: `internal/model/pipeline.go`

- [ ] **Step 1: Add types**

Open `internal/model/pipeline.go` and append after the existing `Pipeline` struct:

```go
// CoordinateType identifies how coordinate data is encoded in action_execution_metadata.
type CoordinateType string

const (
	CoordinateTypeArrayOfPairs   CoordinateType = "array_of_pairs"
	CoordinateTypeSinglePair     CoordinateType = "single_pair"
	CoordinateTypeObject         CoordinateType = "object"
	CoordinateTypeArrayOfObjects CoordinateType = "array_of_objects"
	CoordinateTypeGeoJSONPoint   CoordinateType = "geojson_point"
)

// MapConfig is embedded in a pipeline's Config JSON under the key "map_config".
// If CoordinateKey is empty the pipeline produces no map pins.
type MapConfig struct {
	CoordinateKey  string         `json:"coordinate_key"`
	CoordinateType CoordinateType `json:"coordinate_type"`
}

// pipelineConfigWithMap is used only for extracting map_config from a pipeline config blob.
type pipelineConfigWithMap struct {
	MapConfig *MapConfig `json:"map_config"`
}

// ParseMapConfig extracts the MapConfig from a pipeline config blob.
// Returns nil if absent, invalid JSON, or CoordinateKey is empty.
func ParseMapConfig(config json.RawMessage) *MapConfig {
	if len(config) == 0 {
		return nil
	}
	var c pipelineConfigWithMap
	if err := json.Unmarshal(config, &c); err != nil || c.MapConfig == nil {
		return nil
	}
	if c.MapConfig.CoordinateKey == "" {
		return nil
	}
	return c.MapConfig
}
```

- [ ] **Step 2: Verify it compiles**

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: build succeeds (no Go errors).

- [ ] **Step 3: Commit**

```bash
git add internal/model/pipeline.go
git commit -m "feat: add MapConfig types and ParseMapConfig helper to model"
```

---

### Task 2: Update `MapPin` struct and `ListMapPins` SQL

**Files:**
- Modify: `internal/store/store.go` (lines 75–82)
- Modify: `internal/store/sqlite.go` (lines 1538–1581)

- [ ] **Step 1: Add `CoordinateType` to `MapPin`**

In `internal/store/store.go`, find the `MapPin` struct (around line 75) and replace it:

```go
// MapPin is a geo-event pin derived from action_execution_metadata.
type MapPin struct {
	TweetID         string                `json:"tweet_id"`
	TweetCreatedAt  string                `json:"tweet_created_at"`
	ModelName       string                `json:"model_name"`
	Description     string                `json:"description,omitempty"`
	CoordinatesJSON string                `json:"-"` // raw JSON, format indicated by CoordinateType
	CoordinateType  model.CoordinateType  `json:"-"`
}
```

- [ ] **Step 2: Update `ListMapPins` SQL in `internal/store/sqlite.go`**

Find `func (s *sqliteStore) ListMapPins` (around line 1538) and replace the entire function:

```go
func (s *sqliteStore) ListMapPins(ctx context.Context) ([]*MapPin, error) {
	query := `
        SELECT ae.tweet_id,
               COALESCE(t.created_at, ae.executed_at) AS tweet_created_at,
               p.name AS model_name,
               COALESCE(aem_desc.value, '') AS description,
               aem_loc.value AS coordinates_json,
               COALESCE(json_extract(p.config, '$.map_config.coordinate_type'), '') AS coordinate_type
        FROM action_execution_metadata aem_loc
        JOIN action_executions ae ON ae.id = aem_loc.execution_id AND ae.status = 'success'
        JOIN (
            SELECT tweet_id, action_id, MAX(id) AS latest_id
            FROM action_executions
            WHERE status = 'success'
            GROUP BY tweet_id, action_id
        ) latest ON latest.latest_id = ae.id
        JOIN actions a ON a.id = ae.action_id
        JOIN pipelines p ON p.id = a.pipeline_id
        LEFT JOIN tweets t ON t.id = ae.tweet_id
        LEFT JOIN action_execution_metadata aem_desc
               ON aem_desc.execution_id = ae.id AND aem_desc.key = 'description'
        WHERE aem_loc.key = json_extract(p.config, '$.map_config.coordinate_key')
          AND json_extract(p.config, '$.map_config.coordinate_key') IS NOT NULL
          AND json_extract(p.config, '$.map_config.coordinate_key') != ''
          AND aem_loc.value != 'null'
          AND aem_loc.value != '[]'
        ORDER BY tweet_created_at DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: list map pins: %w", err)
	}
	defer rows.Close()

	var pins []*MapPin
	for rows.Next() {
		var pin MapPin
		var coordType string
		if err := rows.Scan(
			&pin.TweetID, &pin.TweetCreatedAt, &pin.ModelName,
			&pin.Description, &pin.CoordinatesJSON, &coordType,
		); err != nil {
			return nil, fmt.Errorf("store: list map pins: scan: %w", err)
		}
		pin.CoordinateType = model.CoordinateType(coordType)
		pins = append(pins, &pin)
	}
	return pins, rows.Err()
}
```

- [ ] **Step 3: Verify it compiles**

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: build succeeds.

- [ ] **Step 4: Commit**

```bash
git add internal/store/store.go internal/store/sqlite.go
git commit -m "feat: add coordinate_type to MapPin and use json_extract in ListMapPins SQL"
```

---

### Task 3: Write `parseCoordinates` tests

**Files:**
- Create: `internal/api/handlers_map_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/api/handlers_map_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to confirm they fail (function not yet defined)**

```bash
docker compose run --rm iran-watcher go test ./internal/api/ -run TestParseCoordinates -v 2>&1 | tail -15
```

Expected: compilation error — `parseCoordinates undefined`.

- [ ] **Step 3: Commit the test file**

```bash
git add internal/api/handlers_map_test.go
git commit -m "test: add parseCoordinates unit tests (failing)"
```

---

### Task 4: Implement `parseCoordinates` and wire into handler

**Files:**
- Modify: `internal/api/handlers_map.go`

- [ ] **Step 1: Add `parseCoordinates` and update the handler**

Replace the entire content of `internal/api/handlers_map.go`:

```go
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
```

- [ ] **Step 2: Run the tests**

```bash
docker compose run --rm iran-watcher go test ./internal/api/ -run TestParseCoordinates -v 2>&1 | tail -20
```

Expected: all 7 test cases PASS.

- [ ] **Step 3: Run all unit tests**

```bash
docker compose run --rm iran-watcher go test ./... 2>&1 | tail -10
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_map.go
git commit -m "feat: implement parseCoordinates for 5 coordinate types in map handler"
```

---

### Task 5: Frontend — reactive state, load, and save

**Files:**
- Modify: `web/src/views/PipelinesView.vue`

- [ ] **Step 1: Add reactive refs**

In `PipelinesView.vue`, find the block of `ref` declarations near the top of `<script setup>` (around line 42–49, after `formTransformers`). Add these two lines after `const formTransformers = ref<TransformerSpec[]>([])`:

```ts
const formMapCoordinateKey  = ref('')
const formMapCoordinateType = ref('array_of_pairs')
```

Also add the coordinate type options constant after the `transformerTypeOptions` array (around line 75):

```ts
const coordinateTypeOptions = [
  { label: 'Array of pairs  [[lat,lon],...]',          value: 'array_of_pairs'   },
  { label: 'Single pair  [lat,lon]',                   value: 'single_pair'      },
  { label: 'Object  {"lat":x,"lon":x}',                value: 'object'           },
  { label: 'Array of objects  [{"lat":x,"lon":x},...]', value: 'array_of_objects' },
  { label: 'GeoJSON Point',                            value: 'geojson_point'    },
]
```

- [ ] **Step 2: Reset in `openCreate`**

In `openCreate()` (around line 176), add these two lines before `showForm.value = true`:

```ts
  formMapCoordinateKey.value = ''
  formMapCoordinateType.value = 'array_of_pairs'
```

- [ ] **Step 3: Load in `openEdit`**

In `openEdit()` (around line 198), add these two lines before `showForm.value = true` (after the last notification-specific field):

```ts
  formMapCoordinateKey.value  = row.config?.map_config?.coordinate_key  ?? ''
  formMapCoordinateType.value = row.config?.map_config?.coordinate_type ?? 'array_of_pairs'
```

- [ ] **Step 4: Save in `onSave`**

In `onSave()`, find the block that handles transformers (around line 331, after the type-specific branches close). After the transformer block (after the closing `}` of the `if (formTransformers.value.length > 0)` block), add:

```ts
  if (formMapCoordinateKey.value.trim()) {
    config.map_config = {
      coordinate_key:  formMapCoordinateKey.value.trim(),
      coordinate_type: formMapCoordinateType.value,
    }
  }
```

- [ ] **Step 5: Commit**

```bash
git add web/src/views/PipelinesView.vue
git commit -m "feat: add map_config reactive state, load, and save to pipeline form"
```

---

### Task 6: Frontend — add Map Output UI fields

**Files:**
- Modify: `web/src/views/PipelinesView.vue`

- [ ] **Step 1: Add the UI section**

In the `<template>` block of `PipelinesView.vue`, find the Transformers divider/section (around line 631):

```html
      <!-- Transformers — available for all pipeline types -->
      <n-divider style="margin: 4px 0" />
      <n-form-item label="Transformers">
```

Insert the following Map Output section **before** that Transformers divider:

```html
      <!-- Map Output — available for all pipeline types -->
      <n-divider style="margin: 4px 0">Map Output <span style="font-size:11px;color:#6c7086">(optional)</span></n-divider>
      <n-form-item label="Coordinate key">
        <n-input
          v-model:value="formMapCoordinateKey"
          placeholder="e.g. geo_coordinates"
          clearable
        />
      </n-form-item>
      <n-form-item v-if="formMapCoordinateKey.trim()" label="Coordinate type">
        <n-select
          v-model:value="formMapCoordinateType"
          :options="coordinateTypeOptions"
        />
      </n-form-item>

```

- [ ] **Step 2: Build and verify**

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/views/PipelinesView.vue
git commit -m "feat: add Map Output section to pipeline form with coordinate key/type fields"
```

---

### Task 7: Final build verification and CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Full build**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -10
```

Expected: exits 0.

- [ ] **Step 2: Update CLAUDE.md**

In `CLAUDE.md`, find the **Map date-heat coloring** entry in Key Design Decisions. Append a sentence to it:

```
- **Map coordinate configuration** — each pipeline can declare which `action_execution_metadata` key holds its coordinate output and the format of that value via a `map_config: { coordinate_key, coordinate_type }` section in the pipeline's `config` JSON; the five supported types are `array_of_pairs` ([[lat,lon],...]), `single_pair` ([lat,lon]), `object` ({"lat":x,"lon":x}), `array_of_objects` ([{"lat":x,"lon":x},...]), and `geojson_point` (GeoJSON Point, lon/lat swapped on parse); pipelines without a `map_config` key produce no pins; configured in the Pipelines UI form under "Map Output"
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for configurable map metadata"
```
