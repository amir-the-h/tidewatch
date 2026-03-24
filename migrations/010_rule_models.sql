CREATE TABLE IF NOT EXISTS rule_models (
    rule_id  INTEGER NOT NULL REFERENCES rules(id)  ON DELETE CASCADE,
    model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, model_id)
);
