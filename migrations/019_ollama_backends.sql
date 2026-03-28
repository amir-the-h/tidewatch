CREATE TABLE ollama_backends (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    url        TEXT    NOT NULL UNIQUE,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE pipeline_backends (
    pipeline_id INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    backend_id  INTEGER NOT NULL REFERENCES ollama_backends(id) ON DELETE CASCADE,
    priority    INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (pipeline_id, backend_id)
);
