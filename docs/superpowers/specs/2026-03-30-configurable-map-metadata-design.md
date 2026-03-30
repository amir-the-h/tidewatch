# Configurable Map Metadata

**Date:** 2026-03-30
**Status:** Approved

## Problem

`ListMapPins` hardcodes `WHERE aem_loc.key = 'coordinates'` and always parses values as `[][2]float64`. Pipelines that store coordinates under a different key or in a different format cannot contribute pins to the map.

## Goal

Allow each pipeline to declare, via its existing `config` JSON, which metadata key holds coordinates and what format that value takes. UI exposes this as two optional form fields — no raw JSON editing required.

## Data Model

A `MapConfig` struct is added to `internal/model/pipeline.go`:

```go
type CoordinateType string

const (
    CoordinateTypeArrayOfPairs   CoordinateType = "array_of_pairs"
    CoordinateTypeSinglePair     CoordinateType = "single_pair"
    CoordinateTypeObject         CoordinateType = "object"
    CoordinateTypeArrayOfObjects CoordinateType = "array_of_objects"
    CoordinateTypeGeoJSONPoint   CoordinateType = "geojson_point"
)

type MapConfig struct {
    CoordinateKey  string         `json:"coordinate_key"`
    CoordinateType CoordinateType `json:"coordinate_type"`
}
```

Stored inside the pipeline's `config` JSON under the key `"map_config"`:

```json
{
  "map_config": {
    "coordinate_key": "geo_coordinates",
    "coordinate_type": "array_of_pairs"
  }
}
```

Pipelines without a `map_config` key produce no map pins. No DB migration required.

## Coordinate Types

| Type | Wire format | Notes |
|------|-------------|-------|
| `array_of_pairs` | `[[lat,lon],...]` | Current default (existing pipelines) |
| `single_pair` | `[lat,lon]` | Single location per execution |
| `object` | `{"lat":x,"lon":x}` | Single location as named fields |
| `array_of_objects` | `[{"lat":x,"lon":x},...]` | Multiple locations as named fields |
| `geojson_point` | `{"type":"Point","coordinates":[lon,lat]}` | GeoJSON — note lon/lat order, swapped on parse |

All formats are normalized to `[][2]float64` (lat, lon) before returning to the frontend.

## Backend Changes

### `internal/model/pipeline.go`
Add `CoordinateType`, `MapConfig` types. Add a `ParseMapConfig(config json.RawMessage) *MapConfig` helper that extracts `map_config` from a pipeline config blob, returning nil if absent or empty key.

### `internal/store/store.go`
Add `CoordinateType string` field to `MapPin` struct.

### `internal/store/sqlite.go` — `ListMapPins`
Replace the hardcoded `WHERE aem_loc.key = 'coordinates'` filter with a dynamic join on the pipeline's config:

```sql
WHERE aem_loc.key = json_extract(p.config, '$.map_config.coordinate_key')
  AND json_extract(p.config, '$.map_config.coordinate_key') IS NOT NULL
  AND json_extract(p.config, '$.map_config.coordinate_key') != ''
```

Also `SELECT json_extract(p.config, '$.map_config.coordinate_type') AS coordinate_type` and scan it into `pin.CoordinateType`.

### `internal/api/handlers_map.go`
Replace the direct `json.Unmarshal` into `[][2]float64` with a `parseCoordinates(raw string, t CoordinateType) ([][2]float64, error)` function:

- `array_of_pairs` → unmarshal as `[][2]float64` directly
- `single_pair` → unmarshal as `[2]float64`, wrap in slice
- `object` → unmarshal as `struct{ Lat, Lon float64 }`, wrap in slice
- `array_of_objects` → unmarshal as `[]struct{ Lat, Lon float64 }`, map to pairs
- `geojson_point` → unmarshal as `struct{ Coordinates [2]float64 }`, swap lon/lat, wrap in slice
- empty/unknown type → fall back to `array_of_pairs` behavior (coordinate_type is optional in the config; omitting it implies array_of_pairs)

## Frontend Changes

### `web/src/views/PipelinesView.vue`

Add two reactive form fields:

```ts
const formMapCoordinateKey  = ref('')
const formMapCoordinateType = ref<string>('array_of_pairs')
```

**Load (edit mode):** extract `config.map_config` from the pipeline's config JSON and populate both fields. If absent, leave as defaults.

**Save:** if `formMapCoordinateKey` is non-empty, merge `{ map_config: { coordinate_key, coordinate_type } }` into the config JSON before sending. If empty, omit `map_config` entirely.

**UI (in the pipeline form, below existing fields):**

```
── Map Output (optional) ──────────────────────────
Coordinate key   [________________]   ← NInput, placeholder "e.g. geo_coordinates"
Coordinate type  [array_of_pairs ▼]   ← NSelect, only shown when key is non-empty
```

The `NSelect` options map to the 5 `CoordinateType` values with human-readable labels.

## API Contract

`GET /api/map` response shape is unchanged: `coordinates: [number, number][]`. The frontend (`MapView.vue`, `web/src/api/map.ts`) requires no changes.

## Backward Compatibility

Existing pipelines that previously used the hardcoded `coordinates` key will stop producing pins unless they add a `map_config` with `coordinate_key: "coordinates"`. This is an intentional breaking change — operators must opt in per-pipeline. Any pipeline that previously relied on the hardcoded key should be updated via the UI.

## Out of Scope

- Configuring the `description` companion metadata key (stays hardcoded as `"description"`)
- Per-action map config overrides
- Map config validation on save (empty type with a non-empty key defaults to `array_of_pairs`)
