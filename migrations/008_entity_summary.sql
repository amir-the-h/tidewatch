-- Add summary column to entity_tracks for summarizer model output.
ALTER TABLE entity_tracks ADD COLUMN summary TEXT NOT NULL DEFAULT '';
