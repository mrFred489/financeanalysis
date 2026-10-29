-- +goose Up
-- SQL in this section is executed when the migration is applied

CREATE TABLE IF NOT EXISTS setting (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS category (
    id INTEGER PRIMARY KEY,
    key TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    color TEXT NOT NULL
) STRICT;

-- Seed the category table with the four fixed categories
INSERT INTO category (id, key, name, color) VALUES
    (1, 'income', 'Income', '#00b894'),
    (2, 'fixed', 'Fixed costs', '#e17055'),
    (3, 'variable', 'Variable spending', '#6c5ce7'),
    (4, 'savings', 'Savings', '#fd79a8')
ON CONFLICT DO NOTHING;

-- Create goose version table
CREATE TABLE IF NOT EXISTS goose_db_version (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id INTEGER NOT NULL UNIQUE,
    is_applied BOOLEAN NOT NULL DEFAULT 1,
    tstamp TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);

-- +goose Down
-- SQL in this section is executed when the migration is rolled back

DROP TABLE IF EXISTS setting;
DROP TABLE IF EXISTS category;
