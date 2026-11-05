-- +goose Up

-- M2 undo/redo action log (PRD section 6, plan docs/plans/M2-manual-categorization.md D1/D8).
-- action_log is one undoable user action; action_change stores a whole-row JSON
-- snapshot (before_json/after_json) per affected row, so undo restores rows by ID
-- and works across reloads and restarts (UNDO-2).
-- NOTE for future migrations (M2 risk register): never drop/rename columns of
-- tables referenced here (statement_line) — undo restores snapshots by column name.

CREATE TABLE action_log (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    undone_at TEXT
) STRICT;

-- LIFO order = id order; the PK index covers it (UNDO-3).
CREATE TABLE action_change (
    action_id INTEGER NOT NULL REFERENCES action_log(id) ON DELETE CASCADE,
    entity TEXT NOT NULL,
    entity_id INTEGER NOT NULL,
    before_json TEXT NOT NULL,
    after_json TEXT NOT NULL,
    PRIMARY KEY (action_id, entity, entity_id)
) STRICT;

-- +goose Down

-- Dev-only rollback.
DROP TABLE action_change;
DROP TABLE action_log;
