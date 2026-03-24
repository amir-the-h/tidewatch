CREATE TABLE IF NOT EXISTS model_labels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    model_name TEXT NOT NULL,
    label      TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(model_name, label)
);
