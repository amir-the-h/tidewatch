# Tidewatch vision — beyond X

Tidewatch started as an X (Twitter) Filtered Stream monitor. This document describes where it is going: a self-hosted situational-awareness system where X is **one source among many**, alongside news feeds, Telegram channels, conflict datasets, and live ship and aircraft movements. Everything a source produces flows through the same configurable flow builder.

This is a planning document. Nothing here is implemented yet. Each phase gets its own design pass before code is written, and CLAUDE.md / README.md are updated as phases land.

## Inspiration and licence boundary

The source mix is inspired by [koala73/worldmonitor](https://github.com/koala73/worldmonitor), a real-time global intelligence dashboard. What Tidewatch borrows is the idea of combining these inputs:

- **Ships (AIS):** live vessel positions, watch areas around chokepoints (Hormuz, Bab el-Mandeb, Suez…), "dark ship" detection when a vessel stops transmitting, naval vessel identification from the MMSI country code, name prefixes (USS, HMS…) and callsigns.
- **Aircraft (ADS-B):** military flight tracking, emergency squawks, airspace closures (NOTAMs), GPS-jamming areas.
- **War-zone news:** curated conflict events (ACLED, UCDP, GDELT), rocket and drone siren alerts (OREF), strike reports pulled from Telegram channels (keyword pre-filter, then an AI classifier).
- **News sources:** many RSS feeds by region, each tagged with a reliability tier, propaganda risk and state affiliation, and clustered so one story from many outlets reads as one story.

Out of scope: worldmonitor's SaaS layer (billing, accounts, hosted API), its finance and market panels, and its multi-site variants.

**worldmonitor is AGPL-3.0 and Tidewatch is MIT.** Tidewatch takes ideas only. Its code and curated data files (feed lists, vessel databases, Telegram channel lists, keyword tables) are never copied in. Tidewatch builds its own from primary sources.

## Core model

### Three kinds of data

Today everything is a tweet: it is stored, queued, run through a pipeline and its outputs saved. Ship and aircraft positions do not fit that model. AIS alone can produce thousands of position updates a minute, and running each one through Redis tasks and writing it to SQLite (on NFS) would overwhelm the system. So the data splits in three:

| Kind | Examples | Storage | Goes through pipelines? |
|---|---|---|---|
| **Signals** | tweets, RSS articles, Telegram posts, GDELT/ACLED events, siren alerts, detections | SQLite `signals` | Yes — every signal is a pipeline input |
| **Tracks** | vessels, aircraft | Redis (live + short history), SQLite (thinned-out history) | No — positions stay in the track layer |
| **Zones** | polygons around chokepoints, borders, bases, cities | SQLite `zones` | No — a resource that detectors, connectors and the map use |

**Signal** is the generic noun that replaces "tweet" across the backend, API, CLI, MCP tools and UI. A detection from the track layer ("tanker went dark in Hormuz") is a signal too, so a single trigger model covers text and movement.

### Signals

`signals` replaces `tweets`:

| Column | Notes |
|---|---|
| `id` | internal ID |
| `source_id` | the source instance that produced it |
| `kind` | `post`, `article`, `event`, `alert`, `track.zone_entered`, `track.dark`, … |
| `external_id` | upstream ID (tweet ID, GUID, ACLED event ID…); unique per source type |
| `author`, `title`, `text`, `url`, `lang` | optional; text sources fill most of them |
| `published_at`, `fetched_at` | `published_at` drives date-heat colouring and ordering |
| `lat`, `lon` | optional; structured sources (ACLED, FIRMS, detections) arrive with coordinates |
| `track_id` | set for detections, links back to the vessel or aircraft |
| `raw_json` | the upstream payload, untouched |

Deduplication: `(source_type, external_id)` is unique; a content hash catches the same article arriving through two feeds.

The junction table becomes `signal_sources`. Every `tweet_id` column (`action_executions`, `action_execution_metadata`, `pipeline_run_tracking`, `pipeline_node_executions`, map pins…) becomes `signal_id`, and `queue.Task` carries a signal reference. This rename is the largest single change in the plan (about 27 Go files today) and is done once, in phase 1.

### Sources and connectors

A **connector** is code that knows how to talk to one upstream. A **source** is a configured instance of a connector (which feed, which channel, which zone, which credentials), managed at runtime like notification providers are today.

```go
type Connector interface {
    Type() string                 // "rss", "x_stream", "adsb_lol", …
    Mode() Mode                   // Stream | Poll | Backfill
    Run(ctx context.Context, src Source, out Emitter) error
}
```

- **Stream** — long-lived connections: X Filtered Stream, aisstream.io WebSocket, Telegram MTProto.
- **Poll** — scheduled fetches: RSS/Atom, adsb.lol, GDELT 15-minute export, OREF, NOTAMs, FIRMS.
- **Backfill** — on-demand historical fetch: X search.

The `Emitter` takes either signals (persisted, then dispatched) or track positions (handed to the track layer). Credentials live on resource pages (as X auth and notification providers do now), not inside flows.

Source metadata, available to flow conditions:

- `tier` — 1 wire services and official bodies, 2 major outlets, 3 specialist and regional, 4 aggregators and blogs.
- `state_affiliated` — boolean.
- `region`, `language` — free tags.

That makes rules like "alert only when tier ≤ 2, or two independent sources agree" possible in the flow builder.

X becomes two connectors, `x_stream` and `x_search`. The existing rule builder becomes the configuration for `x_stream` sources.

### Triggers

- `new_tweet` → `new_signal`, filterable by source type, signal kind and tier.
- Detections arrive as `new_signal` with a `track.*` kind, so "military aircraft entered zone X" is an ordinary flow from a Start node with a kind filter.
- `new_categorization`, `entity_matched`, `pipeline_finished`, `digest_requested` and `chain_only` keep their meaning with `signal_id` in place of `tweet_id`.

This fits the per-trigger flow model and the one-flow-builder plan already under way: each trigger owns its flows, actions are defined inside the builder, and resource pages (Ollama, notifications, sources, zones) stay separate.

### Tracks

A track is one vessel (keyed by MMSI) or one aircraft (keyed by ICAO 24-bit hex address), with static data (name, callsign, type, flag, military flag) and a position history.

**Live state and short history — Redis**

- `tidewatch:track:{id}` — hash with the latest position, speed, heading, altitude, timestamp and static data; TTL so stale tracks expire.
- `tidewatch:track:{id}:hist` — list of recent positions, trimmed to a few hours. Used for trails on the map and by detectors that need recent motion (loitering, gaps).
- `tidewatch:tracks:zone:{zone_id}` — set of tracks currently inside each zone, used for entry and exit detection.

**Thinned-out history — SQLite**

`track_points (track_id, ts, lat, lon, speed, heading, alt)`, indexed by `(track_id, ts)`, kept for later analysis (routes, port calls, patrol patterns). A point is written when any of these hold:

- N minutes have passed since the last stored point (default 5–10);
- heading or speed changed beyond a threshold;
- the track entered or left a zone.

Points are buffered and written in batches, so the NFS-backed database sees occasional bulk writes rather than a constant stream. `tracks` holds the static data. Retention for `track_points` is configurable.

### Zones

Polygons with a name, tags and a purpose:

- **Detection** — geofence for entry, exit, loitering and gap detectors.
- **Collection** — limits what connectors pull: aisstream.io subscribes by bounding box; adsb.lol and airplanes.live take point-and-radius queries. Without receivers of its own, Tidewatch only sees what zones ask for.
- **Filtering** — map views, digests and conflict datasets scoped to a zone.

Zones are drawn on the map (phase 6). Until then they are created through the API, CLI or MCP tools from GeoJSON.

### Detectors

Detectors run inside the track layer on every position update (or on a timer, for gaps). They emit detection signals:

| Detector | Signal kind | Notes |
|---|---|---|
| Zone entry / exit | `track.zone_entered`, `track.zone_left` | per zone, with optional filters (military only, vessel type) |
| AIS gap | `track.dark`, `track.reappeared` | no position for > N minutes while last seen inside a zone |
| Loitering | `track.loitering` | low speed / small radius for a sustained period |
| Rendezvous | `track.rendezvous` | two vessels close together at low speed (possible ship-to-ship transfer) |
| Emergency squawk | `track.squawk` | 7500 (hijack), 7600 (radio failure), 7700 (emergency) |
| Military classification | `track.military_seen` | adsb.lol military flag, MMSI country code + naval name prefix, callsign patterns, user watchlist |

Detectors have cool-downs so a track bouncing on a zone edge does not flood the queue.

## Sources by phase

| Source | Connector | Mode | Auth | Notes |
|---|---|---|---|---|
| X Filtered Stream / search | `x_stream`, `x_search` | Stream / Backfill | bearer token | existing code, rewrapped |
| RSS / Atom | `rss` | Poll | none | first new connector; one source per feed |
| Telegram channels | `telegram` | Stream | MTProto session | pure Go via `gotd/td`, no Python |
| GDELT 2.0 | `gdelt` | Poll (15 min) | none | event export + DOC API |
| OREF sirens | `oref` | Poll | none | geo-blocked outside Israel; may need a proxy |
| adsb.lol | `adsb_lol` | Poll | none | military feed + per-zone point queries; data is ODbL |
| airplanes.live / adsb.fi | `airplanes_live`, `adsb_fi` | Poll | none | gap-fill; non-commercial use only |
| aisstream.io | `aisstream` | Stream | API key | WebSocket, bounding-box subscriptions per zone |
| ACLED | `acled` | Poll | account | conflict and protest events; terms limit redistribution |
| UCDP | `ucdp` | Poll | none | candidate events (monthly) |
| NASA FIRMS | `firms` | Poll | map key | fire and heat detections, useful for confirming strikes |
| NOTAMs | `notam` | Poll | depends on provider | airspace closures |
| NGA navigational warnings | `nga_navwarn` | Poll | none | maritime safety warnings |

**Coverage caveat.** Tidewatch has no ADS-B or AIS receivers of its own, so it sees only what volunteer networks pick up. Military aircraft over the Middle East are reasonably covered. Ship coverage in the Persian Gulf is patchy, and warships often switch AIS off. Connectors keep the door open for a local receiver later (readsb and AIS-catcher speak standard formats), but nothing is built for one now.

## Phases

### 1. Generic signals, connector framework, RSS

Detailed design: [phase-1-signals.md](phase-1-signals.md).

- Schema reset (no backward compatibility; the old database is disposable): `tweets` → `signals`, `signal_sources` junction, every `tweet_id` → `signal_id`.
- `Connector` interface, source instances, source registry and runtime source management (API, CLI, MCP, UI page).
- X rewrapped as `x_stream` + `x_search`.
- RSS/Atom connector.
- `new_tweet` → `new_signal` with kind/source filters.
- Source metadata (`tier`, `state_affiliated`, `region`, `language`).
- UI and API wording: "Tweets" → "Signals".

Done when an RSS article and a tweet go through the same flow and show up side by side on the Signals page.

### 2. More text sources

- Telegram (`gotd/td`), GDELT, OREF.
- Cross-source story clustering with a sentence-embedding ONNX model, so one story from many outlets groups together; corroboration ("two independent sources") becomes a condition.

### 3. Zones and the track layer

- `zones` resource (GeoJSON in through API, CLI and MCP).
- Track layer: Redis live state and short history, thinned SQLite history (`tracks`, `track_points`) with batched writes and retention.
- adsb.lol first (simple polling, free military feed), then aisstream.io.
- Track API (`GET /api/tracks`, `GET /api/tracks/{id}`, history queries) and a WebSocket `track_update` event, ready for the map.

### 4. Detectors

- The detectors above, emitting `track.*` signals with cool-downs.
- Example flows in `docs/examples/`: "military aircraft enters zone → notify", "vessel dark in Hormuz for 2 h → notify + digest".

### 5. Conflict datasets and correlation

- ACLED, UCDP, FIRMS, NOTAM, NGA warnings.
- Correlation: signals in the same zone and time window (a FIRMS hotspot, a Telegram strike report, a NOTAM closure) linked into one incident.
- Per-zone digests: the existing digest job scoped to a zone and time window.

### 6. Map rebuild on MapLibre

- Replace Leaflet with MapLibre GL.
- Layers: signals (date-heat pins, as today), live tracks with trails (WebSocket), zones, detections, conflict events; toggled independently.
- Zone drawing and editing on the map.
- Fix map performance. Today's slowness is the `ListMapPins` query (13–26 s per page in production), not rendering, so it needs an index or a precomputed pins table alongside the move to MapLibre.

Until phase 6, tracks and detections are checked through the API, MCP tools and notifications. If that gets painful, a minimal MapLibre view of live tracks can be pulled forward without doing the full rebuild early.

## Constraints that shape the design

- **Pure Go, single binary.** Connectors run in-process; no Python, no sidecars. Telegram uses `gotd/td`.
- **SQLite on NFS, DELETE journal mode.** No high-rate writes: tracks write in batches, live state stays in Redis.
- **Redis lists only** for the task queue. Track state uses hashes, lists and sets under `tidewatch:track*`, separate from `tidewatch:tasks:*`.
- **At-most-once delivery** stays acceptable because signals are persisted before they are queued.
- **Data licences.** adsb.lol is ODbL (attribution, share-alike on the database); airplanes.live and adsb.fi are non-commercial; ACLED limits redistribution. Fine for self-hosted personal use, but a public demo must respect them.
