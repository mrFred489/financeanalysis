-- +goose Up

-- M3 rules (PRD section 6, plan docs/plans/M3-rules.md D1).
-- rule: a regex (plus optional account/direction filters) that assigns a
-- subcategory to matching statement lines (PRD 7.5). Matching itself happens
-- in Go (RE2), not SQL REGEXP (D2/D3).
-- rule_account: the optional account filter; no rows = all accounts.
-- conflict_resolution: the user's remembered winner when several rules match
-- one line (RULE-7); dropped automatically when the rule stops matching
-- (RULE-9, enforced by the evaluator).
-- statement_line.rule_id intentionally has no FK here: dropping a rule must
-- not cascade-delete lines, the evaluator re-points them first (RULE-6).

CREATE TABLE rule (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    pattern TEXT NOT NULL,
    direction TEXT NOT NULL DEFAULT 'both' CHECK (direction IN ('in', 'out', 'both')),
    subcategory_id INTEGER NOT NULL REFERENCES subcategory(id),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

CREATE TABLE rule_account (
    rule_id INTEGER NOT NULL REFERENCES rule(id) ON DELETE CASCADE,
    account_id INTEGER NOT NULL REFERENCES account(id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, account_id)
) STRICT;

CREATE TABLE conflict_resolution (
    line_id INTEGER PRIMARY KEY REFERENCES statement_line(id) ON DELETE CASCADE,
    rule_id INTEGER NOT NULL REFERENCES rule(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- "Lines dropped by this rule" lookups during re-evaluation (RULE-6, plan step 1)
CREATE INDEX idx_statement_line_rule ON statement_line (rule_id);

-- +goose Down

-- Dev-only rollback.
DROP INDEX IF EXISTS idx_statement_line_rule;
DROP TABLE IF EXISTS conflict_resolution;
DROP TABLE IF EXISTS rule_account;
DROP TABLE IF EXISTS rule;
