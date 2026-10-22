-- 003_drop_search_jobs.sql
-- Phase 1 step 3: X search runs as the x_search connector, and manual X
-- searches are source runs (source_runs, kind backfill), so search jobs go.
DROP TABLE search_jobs;
