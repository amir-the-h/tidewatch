# Reverse Geocoding Design

**Date:** 2026-04-11  
**Status:** Approved

## Overview

Add reverse geocoding to the iran-watcher pipeline system: given coordinates extracted from tweets (already stored in `action_execution_metadata`), look up human-readable location names and store them as a parallel metadata key. Uses a self-hosted Nominatim instance to avoid rate limits and costs.

## Components

### 1. Nominatim Docker Service

Add `mediagis/nominatim` to the repo `docker-compose.yml` only (not the production compose). The service imports the Iran regional OSM extract from Geofabrik on first boot. Data is persisted to a bind-mounted local directory `nominatim-data/` in the repo root; the directory contents are gitignored. The service is available on the internal Docker network at `http://nominatim:8088`.

### 2. SQLite Geocode Cache

A new migration adds a `geocode_cache` table:

```sql
CREATE TABLE geocode_cache (
    lat       REAL NOT NULL,
    lon       REAL NOT NULL,
    name      TEXT NOT NULL,
    cached_at DATETIME NOT NULL DEFAULT (strftime('%s','now')),
    PRIMARY KEY (lat, lon)
);
```

Coordinates are rounded to 4 decimal places (~11m precision) before lookup and storage to maximize cache hits for nearby coordinates. `name` stores Nominatim's `display_name`, or an empty string if no meaningful result was returned (open water, empty desert, etc.). No TTL — geography does not change.

### 3. `reverse_geocoder` Internal Pipeline Handler

New file: `internal/pipeline/reverse_geocoder.go`

Implements `inference.Pipeline`. Registered in `serve.go` via:

```go
NewReverseGeocoderFactory(store, cfg.NominatimURL)
```

**Action config (JSON):**

```json
{
  "coordinate_key": "locations",
  "coordinate_type": "array_of_pairs",
  "output_key": "location_names",
  "nominatim_url": "http://nominatim:8088"
}
```

- `coordinate_key`: which upstream metadata key holds coordinates — parsed using the same `parseCoordinates` logic as the map API, supporting all 5 coordinate formats (`array_of_pairs`, `single_pair`, `object`, `array_of_objects`, `geojson_point`); the coordinate type is specified via `coordinate_type` in the action config (mirrors pipeline `map_config.coordinate_type`)
- `output_key`: where to write results
- `nominatim_url`: override for the Nominatim base URL (falls back to app config)

**Execution logic (per tweet):**

1. Read `input.Context[coordinate_key]` — parse as array of `[lat, lon]` pairs
2. For each coordinate:
   a. Round to 4 decimal places
   b. Check `geocode_cache` — return cached name if found
   c. On miss: `GET {nominatim_url}/reverse?lat={lat}&lon={lon}&format=json`, extract `display_name`, write to cache
3. Return `map[string]any{output_key: []string{...}}` — parallel array to input coordinates

Requests are sequential (no goroutines) since Nominatim is local and fast. Empty string in output means no place name found.

**Output example:**

```json
{ "location_names": ["Tehran, Iran", "", "Karaj, Alborz Province, Iran"] }
```

### 4. App Config

New config key: `nominatim_url` (env: `IRAN_WATCHER_NOMINATIM_URL`, default: `http://nominatim:8088`). Passed into the factory at serve.go startup. Individual actions can override via their own `nominatim_url` config field.

## Data Flow

```
tweet arrives
    → geo-extraction action runs
        → saves "locations": [[lat,lon],...] to action_execution_metadata
    → reverse_geocoder action runs (next node in pipeline)
        → reads "locations" from input.Context
        → looks up each coordinate (cache → Nominatim)
        → saves "location_names": ["Tehran, Iran",...] to action_execution_metadata
    → GET /api/map aggregates all metadata per pin
        → "location_names" available in pin.metadata automatically
        → usable in popup templates via ${location_names}
```

## Historical Enrichment

The existing `POST /api/pipelines/{id}/run` backfill endpoint replays historical tweets through any pipeline. No special tooling needed — run the pipeline containing the `reverse_geocoder` node with `new_only: false` to enrich existing geo-events.

## No UI Changes Required

The map API already aggregates all metadata per execution into each pin's `metadata` field. `location_names` appears there automatically. Popup templates and downstream pipeline steps can reference it immediately.

## Files to Create or Modify

| File | Change |
|------|--------|
| `docker-compose.yml` | Add `nominatim` service with bind-mount to `nominatim-data/` |
| `.gitignore` | Add `nominatim-data/*` |
| `migrations/XXX_geocode_cache.sql` | New migration for `geocode_cache` table |
| `internal/pipeline/reverse_geocoder.go` | New internal handler |
| `internal/store/sqlite.go` | Add `GetGeocodeCacheEntry`, `SetGeocodeCacheEntry` methods |
| `internal/store/store.go` | Add methods to `Store` interface |
| `internal/config/config.go` | Add `NominatimURL` field |
| `cli/cmd/serve.go` | Register `reverse_geocoder` factory |
| `CLAUDE.md` | Document new handler, table, and config key |
