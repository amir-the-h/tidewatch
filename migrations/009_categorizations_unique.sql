-- Deduplicate categorizations keeping the latest row per (tweet_id, model_name),
-- then add a unique index so subsequent saves can upsert cleanly.
DELETE FROM categorizations
WHERE id NOT IN (
    SELECT MAX(id) FROM categorizations GROUP BY tweet_id, model_name
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_categorizations_tweet_model
    ON categorizations(tweet_id, model_name);
