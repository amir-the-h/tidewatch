# Reverse Geocoding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `reverse_geocoder` internal pipeline handler that resolves coordinate pairs to human-readable location names via a self-hosted Nominatim instance, caching results in SQLite.

**Architecture:** A new internal pipeline handler reads coordinates from upstream context, looks them up in a SQLite `geocode_cache` table (rounded to 4 d.p.), calls a local Nominatim Docker service on cache miss, and writes the parallel `location_names` string array to `action_execution_metadata`. The coordinate parsing logic is extracted from the map API handler into `internal/model` so both the map API and the reverse geocoder share it.

**Tech Stack:** Go `net/http` (Nominatim client), `modernc.org/sqlite` (cache), `mediagis/nominatim:4.4` Docker image, Iran OSM extract from Geofabrik.

---

## File Map

| File | Change |
|------|--------|
| `nominatim-data/.gitkeep` | New — tracks directory in git |
| `.gitignore` | Add `nominatim-data/*` |
| `docker-compose.yml` | Add `nominatim` service + env var for iran-watcher |
| `migrations/028_geocode_cache.sql` | New migration |
| `internal/model/coordinates.go` | New — exported `ParseCoordinates` (moved from api package) |
| `internal/model/coordinates_test.go` | New — unit tests for ParseCoordinates |
| `internal/api/handlers_map.go` | Remove local `parseCoordinates`, call `model.ParseCoordinates` |
| `internal/store/store.go` | Add `GetGeocodeCache`, `SetGeocodeCache` to Store interface |
| `internal/store/sqlite.go` | Implement the two new methods |
| `internal/store/sqlite_geocode_test.go` | New — unit tests for cache store methods |
| `internal/config/config.go` | Add `Nominatim.URL` field |
| `internal/pipeline/reverse_geocoder.go` | New handler |
| `internal/pipeline/reverse_geocoder_test.go` | New — unit tests |
| `cli/cmd/serve.go` | Register `reverse_geocoder` factory |
| `CLAUDE.md` | Document new handler, table, config key |

---

## Task 1: Setup nominatim-data directory

**Files:**
- Create: `nominatim-data/.gitkeep`
- Modify: `.gitignore`

- [ ] **Step 1: Create nominatim-data directory with gitkeep**

```bash
mkdir -p nominatim-data
touch nominatim-data/.gitkeep
```

- [ ] **Step 2: Add nominatim-data contents to .gitignore**

Open `.gitignore` and add this line after the `data/` entry:

```
# Nominatim PostgreSQL data (bind-mounted Docker volume)
nominatim-data/*
```

- [ ] **Step 3: Verify git tracks the directory but not its future contents**

```bash
git status
```

Expected: `nominatim-data/.gitkeep` appears as an untracked file. The `nominatim-data/` directory itself will be tracked; its contents (added later by Docker) will be ignored.

- [ ] **Step 4: Commit**

```bash
git add nominatim-data/.gitkeep .gitignore
git commit -m "chore: add nominatim-data dir with gitignore for bind-mount volume"
```

---

## Task 2: Add Nominatim service to docker-compose.yml

**Files:**
- Modify: `docker-compose.yml`

- [ ] **Step 1: Add the nominatim service**

Open `docker-compose.yml`. After the `redis` service block and before the `iran-watcher-ui` service block, insert:

```yaml
  nominatim:
    image: mediagis/nominatim:4.4
    container_name: iran-watcher-nominatim
    restart: unless-stopped
    shm_size: 1g
    environment:
      PBF_URL: "https://download.geofabrik.de/asia/iran-latest.osm.pbf"
      REPLICATION_URL: "https://download.geofabrik.de/asia/iran-updates/"
      NOMINATIM_PASSWORD: "nominatim"
    volumes:
      - ./nominatim-data:/var/lib/postgresql/14/main
    networks:
      network:
        ipv4_address: 172.79.0.5
```

> **Note:** On first start, Nominatim imports the OSM extract — this takes several minutes. `iran-watcher` is NOT added to `depends_on` for nominatim; the reverse geocoder handler returns empty strings gracefully when nominatim is unavailable.

- [ ] **Step 2: Add the nominatim URL env var to iran-watcher service**

In the `iran-watcher` service `environment:` block, add after the `IRAN_WATCHER_STREAM_AUTO_CONNECT` line:

```yaml
      # Nominatim reverse geocoding
      IRAN_WATCHER_NOMINATIM_URL: "${IRAN_WATCHER_NOMINATIM_URL:-http://nominatim:8080}"
```

- [ ] **Step 3: Commit**

```bash
git add docker-compose.yml
git commit -m "feat(docker): add self-hosted Nominatim service for reverse geocoding"
```

---

## Task 3: Add geocode_cache migration

**Files:**
- Create: `migrations/028_geocode_cache.sql`

- [ ] **Step 1: Create the migration file**

```sql
-- 028_geocode_cache.sql
-- Cache table for reverse geocoding results (Nominatim).
-- Coordinates are stored rounded to 4 decimal places (~11m precision).
-- Empty name = confirmed "no result" for that coordinate (avoids re-querying).

CREATE TABLE IF NOT EXISTS geocode_cache (
    lat       REAL    NOT NULL,
    lon       REAL    NOT NULL,
    name      TEXT    NOT NULL,
    cached_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    PRIMARY KEY (lat, lon)
);
```

Save to `migrations/028_geocode_cache.sql`.

- [ ] **Step 2: Commit**

```bash
git add migrations/028_geocode_cache.sql
git commit -m "feat(migrations): add geocode_cache table (028)"
```

---

## Task 4: Extract ParseCoordinates to model package

**Files:**
- Create: `internal/model/coordinates.go`
- Create: `internal/model/coordinates_test.go`
- Modify: `internal/api/handlers_map.go`

The `parseCoordinates` function currently lives unexported in `internal/api/handlers_map.go` (lines 54–110). The reverse geocoder lives in `internal/pipeline` and cannot access it. Move it to `internal/model` as an exported function.

- [ ] **Step 1: Write the failing test**

Create `internal/model/coordinates_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go test ./internal/model/... -run TestParseCoordinates -v
```

Expected: compile error — `ParseCoordinates` undefined.

- [ ] **Step 3: Create internal/model/coordinates.go**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/model/... -run TestParseCoordinates -v
```

Expected: all 7 TestParseCoordinates tests PASS.

- [ ] **Step 5: Update handlers_map.go to use model.ParseCoordinates**

In `internal/api/handlers_map.go`:

1. Delete the entire `parseCoordinates` function (lines 54–110 — the block starting with `// parseCoordinates normalises...` through the closing `}`).

2. Find the call site (was line 135, now earlier after deletion):
   ```go
   coords, err := parseCoordinates(pin.CoordinatesJSON, pin.CoordinateType)
   ```
   Replace with:
   ```go
   coords, err := model.ParseCoordinates(pin.CoordinatesJSON, pin.CoordinateType)
   ```

3. Ensure `github.com/amir-the-h/iran-watcher/internal/model` is in the import block. Add it if not present.

- [ ] **Step 6: Verify the api package still compiles**

```bash
go build ./internal/api/...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/model/coordinates.go internal/model/coordinates_test.go internal/api/handlers_map.go
git commit -m "refactor: extract ParseCoordinates to model package"
```

---

## Task 5: Add geocode cache store methods

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/sqlite.go`
- Create: `internal/store/sqlite_geocode_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/store/sqlite_geocode_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestGeocodeCache_SetAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.SetGeocodeCache(ctx, 35.6892, 51.389, "Tehran, Iran"); err != nil {
		t.Fatalf("SetGeocodeCache: %v", err)
	}

	name, found, err := s.GetGeocodeCache(ctx, 35.6892, 51.389)
	if err != nil {
		t.Fatalf("GetGeocodeCache: %v", err)
	}
	if !found {
		t.Fatal("want found=true")
	}
	if name != "Tehran, Iran" {
		t.Errorf("want 'Tehran, Iran', got %q", name)
	}
}

func TestGeocodeCache_Miss(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, found, err := s.GetGeocodeCache(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetGeocodeCache: %v", err)
	}
	if found {
		t.Error("want found=false for missing entry")
	}
}

func TestGeocodeCache_Upsert(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_ = s.SetGeocodeCache(ctx, 35.0, 51.0, "first")
	_ = s.SetGeocodeCache(ctx, 35.0, 51.0, "updated")

	name, found, err := s.GetGeocodeCache(ctx, 35.0, 51.0)
	if err != nil {
		t.Fatalf("GetGeocodeCache: %v", err)
	}
	if !found || name != "updated" {
		t.Errorf("want 'updated', got %q (found=%v)", name, found)
	}
}

func TestGeocodeCache_EmptyNameCached(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Empty string = confirmed "no result" — must be distinguishable from "not in cache".
	_ = s.SetGeocodeCache(ctx, 10.0, 10.0, "")

	name, found, err := s.GetGeocodeCache(ctx, 10.0, 10.0)
	if err != nil {
		t.Fatalf("GetGeocodeCache: %v", err)
	}
	if !found {
		t.Fatal("want found=true for cached empty name")
	}
	if name != "" {
		t.Errorf("want empty name, got %q", name)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/store/... -run TestGeocodeCache -v
```

Expected: compile error — `GetGeocodeCache`/`SetGeocodeCache` undefined on Store.

- [ ] **Step 3: Add methods to Store interface in store.go**

In `internal/store/store.go`, find the `// Map` section (containing `ListMapPins`) and add a new `// GeocodeCache` section after it:

```go
	// GeocodeCache
	// GetGeocodeCache returns the cached location name for a pre-rounded coordinate.
	// found=false means no cache entry exists (distinct from found=true, name="" which
	// means Nominatim confirmed no result for that coordinate).
	GetGeocodeCache(ctx context.Context, lat, lon float64) (name string, found bool, err error)
	// SetGeocodeCache stores a location name for a pre-rounded coordinate.
	// name="" is a valid cached value meaning "Nominatim returned no result here".
	SetGeocodeCache(ctx context.Context, lat, lon float64, name string) error
```

- [ ] **Step 4: Implement the methods in sqlite.go**

Add these two methods to `internal/store/sqlite.go` near the other map-related methods (e.g., after `ListMapPins`):

```go
func (s *sqliteStore) GetGeocodeCache(ctx context.Context, lat, lon float64) (string, bool, error) {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM geocode_cache WHERE lat = ? AND lon = ?`, lat, lon,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: get geocode cache: %w", err)
	}
	return name, true, nil
}

func (s *sqliteStore) SetGeocodeCache(ctx context.Context, lat, lon float64, name string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO geocode_cache (lat, lon, name, cached_at)
		 VALUES (?, ?, ?, strftime('%s','now'))`,
		lat, lon, name,
	)
	if err != nil {
		return fmt.Errorf("store: set geocode cache: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/store/... -run TestGeocodeCache -v
```

Expected: all 4 TestGeocodeCache tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/sqlite.go internal/store/sqlite_geocode_test.go
git commit -m "feat(store): add GetGeocodeCache and SetGeocodeCache methods"
```

---

## Task 6: Add NominatimURL to config

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add Nominatim section to Config struct**

In `internal/config/config.go`, after the `Twitter` struct field (the last field in the struct), add:

```go
	Nominatim struct {
		URL string `yaml:"url"`
	} `yaml:"nominatim"`
```

The env var override is `IRAN_WATCHER_NOMINATIM_URL` (following the existing `IRAN_WATCHER_<SECTION>_<KEY>` pattern).

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/config/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add Nominatim.URL config field"
```

---

## Task 7: Implement reverse_geocoder handler

**Files:**
- Create: `internal/pipeline/reverse_geocoder.go`
- Create: `internal/pipeline/reverse_geocoder_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/pipeline/reverse_geocoder_test.go`:

```go
package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
)

// mockGeoStore implements geocodeCacheStore for testing.
type mockGeoStore struct {
	cache map[[2]float64]string
}

func newMockGeoStore() *mockGeoStore {
	return &mockGeoStore{cache: map[[2]float64]string{}}
}

func (m *mockGeoStore) GetGeocodeCache(_ context.Context, lat, lon float64) (string, bool, error) {
	v, ok := m.cache[[2]float64{lat, lon}]
	return v, ok, nil
}

func (m *mockGeoStore) SetGeocodeCache(_ context.Context, lat, lon float64, name string) error {
	m.cache[[2]float64{lat, lon}] = name
	return nil
}

// nominatimSrv starts an httptest server that always returns the given display_name.
func nominatimSrv(displayName string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"display_name": displayName})
	}))
}

func newTestGeocoder(st *mockGeoStore, nominatimURL string) *ReverseGeocoderPipeline {
	return &ReverseGeocoderPipeline{
		name:  "test-geocoder",
		store: st,
		cfg: reverseGeocoderConfig{
			CoordinateKey:  "locations",
			CoordinateType: "array_of_pairs",
			OutputKey:      "location_names",
			NominatimURL:   nominatimURL,
		},
		client: &http.Client{},
	}
}

func TestReverseGeocoder_MissingContextKey(t *testing.T) {
	p := newTestGeocoder(newMockGeoStore(), "http://unused")

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 0 {
		t.Errorf("want empty slice for missing key, got %v", names)
	}
}

func TestReverseGeocoder_CacheHit(t *testing.T) {
	st := newMockGeoStore()
	// roundCoord(35.68921) = 35.6892; roundCoord(51.38901) = 51.389
	st.cache[[2]float64{35.6892, 51.389}] = "Tehran, Iran"

	p := newTestGeocoder(st, "http://unused")

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"locations": "[[35.68921,51.38901]]"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 1 || names[0] != "Tehran, Iran" {
		t.Errorf("want ['Tehran, Iran'], got %v", names)
	}
}

func TestReverseGeocoder_NominatimCall(t *testing.T) {
	srv := nominatimSrv("Karaj, Alborz Province, Iran")
	defer srv.Close()

	st := newMockGeoStore()
	p := &ReverseGeocoderPipeline{
		name:  "test-geocoder",
		store: st,
		cfg: reverseGeocoderConfig{
			CoordinateKey:  "locations",
			CoordinateType: "array_of_pairs",
			OutputKey:      "location_names",
			NominatimURL:   srv.URL,
		},
		client: srv.Client(),
	}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"locations": "[[35.8324,50.9985]]"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 1 || names[0] != "Karaj, Alborz Province, Iran" {
		t.Errorf("want ['Karaj, Alborz Province, Iran'], got %v", names)
	}

	// Verify result was cached using rounded coords.
	lat := roundCoord(35.8324)
	lon := roundCoord(50.9985)
	cached, found, _ := st.GetGeocodeCache(context.Background(), lat, lon)
	if !found || cached != "Karaj, Alborz Province, Iran" {
		t.Errorf("want cached result, got %q (found=%v)", cached, found)
	}
}

func TestReverseGeocoder_NominatimErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unable to geocode"})
	}))
	defer srv.Close()

	st := newMockGeoStore()
	p := &ReverseGeocoderPipeline{
		name:  "test-geocoder",
		store: st,
		cfg: reverseGeocoderConfig{
			CoordinateKey:  "locations",
			CoordinateType: "array_of_pairs",
			OutputKey:      "location_names",
			NominatimURL:   srv.URL,
		},
		client: srv.Client(),
	}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"locations": "[[0.0,0.0]]"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 1 || names[0] != "" {
		t.Errorf("want [''] for Nominatim error, got %v", names)
	}
	// Verify empty string was cached (so we don't re-query).
	lat, lon := roundCoord(0.0), roundCoord(0.0)
	cached, found, _ := st.GetGeocodeCache(context.Background(), lat, lon)
	if !found || cached != "" {
		t.Errorf("want cached empty string, got %q (found=%v)", cached, found)
	}
}

func TestReverseGeocoder_NominatimDown(t *testing.T) {
	// Point to a non-listening address; handler must not return an error.
	st := newMockGeoStore()
	p := newTestGeocoder(st, "http://127.0.0.1:19999")

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"locations": "[[35.0,51.0]]"},
	})
	if err != nil {
		t.Fatalf("Run must not fail when Nominatim is unreachable, got: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 1 || names[0] != "" {
		t.Errorf("want [''] when Nominatim down, got %v", names)
	}
}

func TestReverseGeocoder_CustomOutputKey(t *testing.T) {
	srv := nominatimSrv("Isfahan, Iran")
	defer srv.Close()

	st := newMockGeoStore()
	p := &ReverseGeocoderPipeline{
		name:  "test-geocoder",
		store: st,
		cfg: reverseGeocoderConfig{
			CoordinateKey:  "coords",
			CoordinateType: "array_of_pairs",
			OutputKey:      "places",
			NominatimURL:   srv.URL,
		},
		client: srv.Client(),
	}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"coords": "[[32.6539,51.6660]]"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := out["places"]; !ok {
		t.Error("want output key 'places'")
	}
	names := out["places"].([]string)
	if len(names) != 1 || names[0] != "Isfahan, Iran" {
		t.Errorf("want ['Isfahan, Iran'], got %v", names)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pipeline/... -run TestReverseGeocoder -v
```

Expected: compile error — `ReverseGeocoderPipeline`, `reverseGeocoderConfig`, `roundCoord` undefined.

- [ ] **Step 3: Create internal/pipeline/reverse_geocoder.go**

```go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// geocodeCacheStore is the subset of store.Store needed by ReverseGeocoderPipeline.
// Using a minimal interface makes the handler easy to test with a simple mock.
type geocodeCacheStore interface {
	GetGeocodeCache(ctx context.Context, lat, lon float64) (string, bool, error)
	SetGeocodeCache(ctx context.Context, lat, lon float64, name string) error
}

// ReverseGeocoderPipeline resolves coordinate pairs to human-readable location names
// via a local Nominatim instance, caching results in SQLite.
//
// Input context key (configurable, default "locations"): raw JSON coordinate value
// in any format supported by model.ParseCoordinates.
//
// Output (configurable, default "location_names"): []string parallel to the input
// coordinate array. Empty string means no place name was found for that coordinate.
type ReverseGeocoderPipeline struct {
	name   string
	store  geocodeCacheStore
	cfg    reverseGeocoderConfig
	client *http.Client
}

type reverseGeocoderConfig struct {
	CoordinateKey  string `json:"coordinate_key"`
	CoordinateType string `json:"coordinate_type"`
	OutputKey      string `json:"output_key"`
	NominatimURL   string `json:"nominatim_url"` // optional per-action override
}

// NewReverseGeocoderFactory returns a Factory that creates ReverseGeocoderPipeline.
// defaultURL is the app-level Nominatim base URL (from config); individual actions
// can override it via their own "nominatim_url" config field.
func NewReverseGeocoderFactory(st store.Store, defaultURL string) Factory {
	return func(name string, config map[string]any) (inference.Pipeline, error) {
		cfg := reverseGeocoderConfig{
			CoordinateKey:  "locations",
			CoordinateType: string(model.CoordinateTypeArrayOfPairs),
			OutputKey:      "location_names",
			NominatimURL:   defaultURL,
		}
		if len(config) > 0 {
			if b, err := json.Marshal(config); err == nil {
				_ = json.Unmarshal(b, &cfg)
			}
		}
		if cfg.NominatimURL == "" {
			cfg.NominatimURL = defaultURL
		}
		return &ReverseGeocoderPipeline{
			name:   name,
			store:  st,
			cfg:    cfg,
			client: &http.Client{Timeout: 10 * time.Second},
		}, nil
	}
}

func (p *ReverseGeocoderPipeline) Name() string                 { return p.name }
func (p *ReverseGeocoderPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *ReverseGeocoderPipeline) Close() error                 { return nil }

func (p *ReverseGeocoderPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	raw, ok := input.Context[p.cfg.CoordinateKey]
	if !ok {
		return map[string]any{p.cfg.OutputKey: []string{}}, nil
	}

	// Normalise context value to a raw JSON string.
	// Context values are strings when they come from MergeAllTweetExecutionMetadata,
	// but may be other types after an in-process JSON round-trip.
	var rawStr string
	if s, ok := raw.(string); ok {
		rawStr = s
	} else {
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("reverse_geocoder: marshal coordinates: %w", err)
		}
		rawStr = string(b)
	}

	pairs, err := model.ParseCoordinates(rawStr, model.CoordinateType(p.cfg.CoordinateType))
	if err != nil {
		return nil, fmt.Errorf("reverse_geocoder: parse coordinates: %w", err)
	}

	names := make([]string, len(pairs))
	for i, pair := range pairs {
		lat := roundCoord(pair[0])
		lon := roundCoord(pair[1])

		if cached, found, err := p.store.GetGeocodeCache(ctx, lat, lon); err == nil && found {
			names[i] = cached
			continue
		}

		name, err := p.callNominatim(ctx, lat, lon)
		if err != nil {
			// Network failure or decode error: leave name empty, do not cache.
			// The next run will retry Nominatim.
			names[i] = ""
			continue
		}

		// Cache both successful names and confirmed empty results (error responses
		// from Nominatim) so we don't re-query the same dead coordinate repeatedly.
		_ = p.store.SetGeocodeCache(ctx, lat, lon, name)
		names[i] = name
	}

	return map[string]any{p.cfg.OutputKey: names}, nil
}

// callNominatim calls the Nominatim /reverse endpoint and returns the display_name.
// Returns ("", nil) when Nominatim responds with an error field (coordinate not found).
// Returns ("", err) on network or decode failure.
func (p *ReverseGeocoderPipeline) callNominatim(ctx context.Context, lat, lon float64) (string, error) {
	url := fmt.Sprintf("%s/reverse?lat=%f&lon=%f&format=json", p.cfg.NominatimURL, lat, lon)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build nominatim request: %w", err)
	}
	req.Header.Set("User-Agent", "iran-watcher/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("nominatim request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		DisplayName string `json:"display_name"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode nominatim response: %w", err)
	}
	if result.Error != "" {
		// Coordinate exists but Nominatim has no name for it (open water, empty desert, etc.)
		return "", nil
	}
	return result.DisplayName, nil
}

// roundCoord rounds a coordinate to 4 decimal places (~11m precision) to maximise
// SQLite cache hits for nearby coordinates.
func roundCoord(v float64) float64 {
	return math.Round(v*10000) / 10000
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/pipeline/... -run TestReverseGeocoder -v
```

Expected: all 6 TestReverseGeocoder tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/reverse_geocoder.go internal/pipeline/reverse_geocoder_test.go
git commit -m "feat(pipeline): add reverse_geocoder internal handler"
```

---

## Task 8: Register factory in serve.go

**Files:**
- Modify: `cli/cmd/serve.go`

- [ ] **Step 1: Register the reverse_geocoder factory**

In `cli/cmd/serve.go`, find the pipeline registry block (around line 133):

```go
	pipeReg := pipeline.NewRegistry()
	pipeReg.Register("entity_matcher", pipeline.NewEntityMatcherFactory(s))
	pipeReg.Register("entity_discovery", pipeline.NewEntityDiscoveryFactory(s))
	pipeReg.Register("entity_cooccurrence", pipeline.NewEntityCooccurrenceFactory(s))
```

After the `entity_cooccurrence` line, add:

```go
	nominatimURL := cfg.Nominatim.URL
	if nominatimURL == "" {
		nominatimURL = "http://nominatim:8080"
	}
	pipeReg.Register("reverse_geocoder", pipeline.NewReverseGeocoderFactory(s, nominatimURL))
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./cli/...
```

Expected: no errors.

- [ ] **Step 3: Run all unit tests to verify nothing broke**

```bash
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add cli/cmd/serve.go
git commit -m "feat(serve): register reverse_geocoder pipeline factory"
```

---

## Task 9: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add reverse_geocoder to internal handlers list**

Find the line in CLAUDE.md listing internal handlers (the bullet list under `internal/pipeline`). Add:

```
- `reverse_geocoder` - Resolves coordinate arrays to location name strings via local Nominatim; caches results in `geocode_cache` (SQLite)
```

- [ ] **Step 2: Add geocode_cache to Key Design Decisions**

In the Key Design Decisions section, add a new bullet (near the map/geo-related decisions):

```
- **Reverse geocoding** — `reverse_geocoder` internal pipeline handler calls a self-hosted `mediagis/nominatim` Docker service (Iran OSM extract from Geofabrik); reads any supported coordinate format from upstream metadata (via `model.ParseCoordinates`), rounds to 4 d.p. for cache keying, stores results in `geocode_cache` SQLite table; returns a `location_names: []string` parallel to the coordinate array (empty string = no name found); gracefully returns empty strings when Nominatim is unavailable; configured per-action via `coordinate_key`, `coordinate_type`, `output_key`, `nominatim_url`; app-level default URL is `nominatim_url` config key (env: `IRAN_WATCHER_NOMINATIM_URL`, default: `http://nominatim:8080`)
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude): document reverse_geocoder handler and geocode_cache table"
```

---

## Task 10: Build verification

- [ ] **Step 1: Build the Docker image**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: build completes successfully with no errors.

- [ ] **Step 2: If the build fails, read the error and fix it before proceeding**

Common issues:
- Missing import in a Go file → add the import
- Type mismatch → re-read the Store interface definition and fix the signature
- Migration embedded FS issue → verify `migrations/028_geocode_cache.sql` is in the `migrations/` directory

- [ ] **Step 3: Commit any build fixes, then verify**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: exit 0.
