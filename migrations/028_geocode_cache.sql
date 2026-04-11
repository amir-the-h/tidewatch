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
