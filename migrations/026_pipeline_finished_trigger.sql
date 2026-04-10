-- migrations/026_pipeline_finished_trigger.sql

-- Track active task count per (pipeline, tweet) for pipeline_finished detection.
CREATE TABLE pipeline_run_tracking (
    pipeline_id   INTEGER NOT NULL,
    tweet_id      TEXT    NOT NULL,
    active_tasks  INTEGER NOT NULL DEFAULT 0,
    event_payload BLOB,
    created_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    completed_at  DATETIME,
    PRIMARY KEY (pipeline_id, tweet_id)
);

-- Allow trigger_pipeline_links for pipeline_finished to scope to a source pipeline.
ALTER TABLE trigger_pipeline_links
    ADD COLUMN source_pipeline_id INTEGER REFERENCES pipelines(id) ON DELETE CASCADE;
