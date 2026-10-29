# Database Migrations Guide

This document describes the migration strategy and best practices for the FinanceAnalysis database schema.

## Overview

FinanceAnalysis uses [goose](https://github.com/pressly/goose) for database migrations with SQLite as the backing store. Each logical schema change is represented by a single SQL file in the `internal/db/migrations/` directory.

## Migration File Naming Convention

Migration files follow the pattern: `NNNNN_description.sql` where:
- `NNNNN` is a 5-digit sequential number (e.g., `00001`, `00002`)
- `description` is a brief, lowercase, hyphen-separated description of the change (e.g., `create_category_table`, `add_subcategory_color`)

Example: `00001_init.sql`, `00002_add_spread_column.sql`

## Best Practices

### 1. One Logical Change Per Migration

Each migration file should contain exactly one logical change. This makes it easier to:
- Understand what each migration does
- Debug issues
- Roll back changes if needed
- Review changes in pull requests

**Do:**
```sql
-- 00002_add_color_to_category.sql
ALTER TABLE category ADD COLUMN color TEXT NOT NULL DEFAULT '#000000';
```

**Don't:**
```sql
-- 00002_add_multiple_columns.sql
ALTER TABLE category ADD COLUMN color TEXT NOT NULL DEFAULT '#000000';
ALTER TABLE category ADD COLUMN icon TEXT;
ALTER TABLE subcategory ADD COLUMN description TEXT;
```

### 2. Never Edit Applied Migrations

Once a migration has been:
- Applied to a real database
- Released in a production version
- Committed to the main branch

**It must never be modified.** If you need to fix a bug or make a change:
1. Create a new migration file with a higher sequence number
2. Fix the issue in the new migration
3. Document the fix in the migration description

### 3. Forward-Only in Practice

While goose supports down migrations for development, the production rollback strategy is **backups**. This is because:
- SQLite has limited ALTER TABLE support
- Down migrations can be complex and error-prone
- Restoring from backup is safer and more predictable

**Down migrations should be written for development testing only.**

### 4. SQLite ALTER TABLE Limitations

SQLite has limited ALTER TABLE support compared to other databases. To work around this, use the **12-step table rebuild procedure**:

```sql
-- +goose NO TRANSACTION

-- Step 1: Disable foreign keys
PRAGMA foreign_keys = OFF;

-- Step 2: Start transaction
BEGIN TRANSACTION;

-- Step 3: Create new table with desired schema
CREATE TABLE new_table_name (
    id INTEGER PRIMARY KEY,
    -- ... all columns with new schema
    new_column TEXT NOT NULL DEFAULT 'default_value',
    FOREIGN KEY (foreign_key_column) REFERENCES other_table(id)
);

-- Step 4: Copy data from old table
INSERT INTO new_table_name (id, col1, col2, ...)
SELECT id, col1, col2, ..., 'default_value' FROM old_table_name;

-- Step 5: Drop old table
DROP TABLE old_table_name;

-- Step 6: Rename new table to old name
ALTER TABLE new_table_name RENAME TO old_table_name;

-- Step 7: Recreate indexes
CREATE INDEX idx_name ON old_table_name (column_name);

-- Step 8: Recreate triggers (if any)
-- ...

-- Step 9: Run integrity checks
PRAGMA integrity_check;
PRAGMA foreign_key_check;

-- Step 10: Re-enable foreign keys
PRAGMA foreign_keys = ON;

-- Step 11: Commit transaction
COMMIT;

-- +goose DOWN
-- SQL for rolling back this migration
```

Note the `+goose NO TRANSACTION` directive tells goose not to wrap this migration in a transaction (since we're managing it ourselves).

### 5. Data Backfills

When a migration requires data changes (e.g., populating a new column based on existing data):

1. **Separate schema changes from data backfills** when practical
2. **Make backfills idempotent** so they can be run multiple times safely
3. **Use transactions** for data backfills to ensure atomicity

Example:
```sql
-- 00003_add_category_color_backfill.sql
-- +goose NO TRANSACTION

BEGIN TRANSACTION;

-- Only update rows where color is NULL
UPDATE category SET color = '#00b894' WHERE key = 'income' AND color IS NULL;
UPDATE category SET color = '#e17055' WHERE key = 'fixed' AND color IS NULL;
UPDATE category SET color = '#6c5ce7' WHERE key = 'variable' AND color IS NULL;
UPDATE category SET color = '#fd79a8' WHERE key = 'savings' AND color IS NULL;

COMMIT;
```

### 6. Seeded/Reference Data

Reference data (e.g., the `category` table) is managed through migrations. When adding or modifying seeded data:
- Create a new migration file
- Use `INSERT ... ON CONFLICT DO UPDATE` or `ON CONFLICT DO NOTHING` as appropriate
- Never modify existing seed data in place

### 7. Use STRICT Tables

All tables should be created with the `STRICT` keyword to enable additional error checking:

```sql
CREATE TABLE example (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    value INTEGER CHECK (value >= 0)
) STRICT;
```

The `STRICT` keyword:
- Prevents implicit type conversions
- Enforces NOT NULL constraints immediately
- Makes CHECK constraints more strict

### 8. Use Explicit CHECK Constraints for Enums

SQLite doesn't have a native ENUM type. Use CHECK constraints instead:

```sql
CREATE TABLE example (
    id INTEGER PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'rejected'))
) STRICT;
```

## Testing Migrations

Every migration must be tested to ensure:

1. **Empty database → Latest version**: The migration can be applied to a fresh database
2. **Fixture database → Latest version**: The migration can be applied to a database from the previous release
3. **Integrity checks pass**: `PRAGMA integrity_check` and `PRAGMA foreign_key_check` return no errors
4. **Round-trip safety**: Up → Down → Up should work for development (though down migrations are not used in production)

### Fixture Databases

Fixture databases from previous releases are stored in `testdata/fixtures/db/`. When cutting a new release:
1. Create a new fixture database from the current schema
2. Commit it to the repository with a descriptive name (e.g., `finance-v1.0.0.sqlite`)

### Migration Test Checklist

```bash
# 1. Test migration on empty database
rm -f testdata/test-empty.sqlite
sqlite3 testdata/test-empty.sqlite "VACUUM;"
goose -dir internal/db/migrations sqlite3 testdata/test-empty.sqlite up
goose -dir internal/db/migrations sqlite3 testdata/test-empty.sqlite verify

# 2. Check integrity
sqlite3 testdata/test-empty.sqlite "PRAGMA integrity_check; PRAGMA foreign_key_check;"

# 3. Test round-trip (dev only)
goose -dir internal/db/migrations sqlite3 testdata/test-empty.sqlite down 0
goose -dir internal/db/migrations sqlite3 testdata/test-empty.sqlite up

# 4. Test on fixture database from previous release
cp testdata/fixtures/db/finance-v1.0.0.sqlite testdata/test-fixture.sqlite
goose -dir internal/db/migrations sqlite3 testdata/test-fixture.sqlite up
goose -dir internal/db/migrations sqlite3 testdata/test-fixture.sqlite verify
sqlite3 testdata/test-fixture.sqlite "PRAGMA integrity_check; PRAGMA foreign_key_check;"
```

## Development Workflow

### Creating a New Migration

1. **Create the migration file** in `internal/db/migrations/` with the next sequential number
2. **Write the Up SQL** to make the schema change
3. **Write the Down SQL** (for development only) to revert the change
4. **Test the migration** locally
5. **Regenerate sqlc code**: `task generate`
6. **Update Go code** if needed to use new tables/columns
7. **Update tests and fixtures**
8. **Update the PRD data model table** if the change affects the schema

### Example: Adding a New Column

```sql
-- +goose Up
ALTER TABLE subcategory ADD COLUMN icon TEXT;

-- +goose Down
ALTER TABLE subcategory DROP COLUMN icon;
```

For SQLite, this simple ALTER TABLE works. But for more complex changes (adding a NOT NULL column, changing column types), use the 12-step procedure.

## Troubleshooting

### Common Issues

1. **`SQLITE_BUSY` errors**: Ensure you're not trying to read and write to the same database connection simultaneously. For a single-user app, set `PRAGMA busy_timeout = 5000` and `MaxOpenConns = 1`.

2. **Foreign key violations**: Run `PRAGMA foreign_key_check` to identify issues. Ensure all foreign key references are valid.

3. **Migration conflicts**: If two developers create migrations with the same sequence number, rename one to have a higher number.

4. **Schema version mismatch**: If the binary's schema version is older than the database, the application will refuse to start with `ErrSchemaTooNew`. Users must upgrade the application.

## Goose Configuration

FinanceAnalysis uses goose with embedded migrations:

```go
import "embed"

//go:embed migrations/*.sql
var embedMigrations embed.FS

provider, err := goose.NewProvider("sqlite3", db, goose.WithFS(embedMigrations))
```

The goose provider is configured to:
- Use SQLite as the database driver
- Read migrations from the embedded filesystem
- Track applied migrations in the `goose_db_version` table

## Useful Commands

```bash
# List all migrations
goose -dir internal/db/migrations sqlite3 ./data/finance.sqlite list

# Show current version
goose -dir internal/db/migrations sqlite3 ./data/finance.sqlite version

# Apply pending migrations
goose -dir internal/db/migrations sqlite3 ./data/finance.sqlite up

# Roll back to a specific version
goose -dir internal/db/migrations sqlite3 ./data/finance.sqlite down <version>

# Verify migrations match
goose -dir internal/db/migrations sqlite3 ./data/finance.sqlite verify
```

## Versioning Strategy

- **Migration versions** are sequential integers (1, 2, 3, ...)
- **Application versions** follow semantic versioning (v1.0.0, v1.1.0, etc.)
- Each release can contain multiple migrations
- The migration version does not need to match the application version
