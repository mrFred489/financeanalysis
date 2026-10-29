-- +goose Up

-- Create setting table
CREATE TABLE IF NOT EXISTS setting (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Create category table
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

-- +goose Down

DROP TABLE IF EXISTS category;
DROP TABLE IF EXISTS setting;
