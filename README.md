# migrate

A simple, library-first PostgreSQL migration tool for Go with **scoped migrations** — multiple modules can manage their own migrations independently in the same database.

## Features

- **Library, not a CLI** — embed directly in your Go application
- **Scoped migrations** — each module/service owns its namespace, no conflicts
- **`fs.FS` interface** — works with `embed.FS`, `os.DirFS`, or `testing/fstest`
- **pgx v5** — built on the modern PostgreSQL driver for Go
- **Advisory locking** — safe for concurrent deploys
- **dbmate-compatible file format** — familiar `-- migrate:up` / `-- migrate:down` sections
- **Transactional** — each migration runs in its own transaction

## Installation

```bash
go get github.com/laenen-partners/migrate
```

## Quick Start

```go
package main

import (
    "context"
    "embed"
    "log"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/laenen-partners/migrate"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
    ctx := context.Background()
    pool, err := pgxpool.New(ctx, "postgres://localhost:5432/mydb")
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    // Apply all pending migrations for the "auth" scope
    if err := migrate.Up(ctx, pool, migrations, "auth"); err != nil {
        log.Fatal(err)
    }
}
```

## API

### `Up`

```go
func Up(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, scope string) error
```

Applies all pending migrations from `fsys` for the given scope, in version order.

### `Down`

```go
func Down(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, scope string, steps int) error
```

Rolls back the most recent `steps` migrations. Pass `0` to roll back all.

## Migration File Format

Files must be named `YYYYMMDDHHMMSS_description.sql` and contain both up and down sections:

```sql
-- migrate:up
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- migrate:down
DROP TABLE users;
```

## Scoped Migrations

The `scope` parameter namespaces migrations so that independent modules can coexist:

```go
// Auth service manages its own migrations
migrate.Up(ctx, pool, authMigrations, "auth")

// Billing service manages its own migrations
migrate.Up(ctx, pool, billingMigrations, "billing")

// Rolling back auth doesn't affect billing
migrate.Down(ctx, pool, authMigrations, "auth", 1)
```

Migration state is tracked in the `scoped_schema_migrations` table:

| Column | Type | Description |
|--------|------|-------------|
| scope | TEXT | Migration namespace |
| version | TEXT | Timestamp from filename |
| applied_at | TIMESTAMPTZ | When the migration was applied |

Primary key: `(scope, version)`

## Concurrency Safety

`Up` and `Down` acquire a PostgreSQL advisory lock (scoped by namespace) before running. Multiple instances deploying simultaneously will serialize safely — the second caller blocks until the first completes.

## Using with `embed.FS`

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS

// embed.FS includes the directory prefix, so use fs.Sub:
subFS, _ := fs.Sub(migrationsFS, "migrations")
migrate.Up(ctx, pool, subFS, "myapp")
```

## Using with `os.DirFS`

```go
migrate.Up(ctx, pool, os.DirFS("./migrations"), "myapp")
```

## Testing

Tests use [testcontainers-go](https://github.com/testcontainers/testcontainers-go) to spin up real PostgreSQL containers. Docker must be running.

```bash
go test -v -count=1 ./...
```

## Development

Prerequisites: [mise](https://mise.jdx.dev/) and [task](https://taskfile.dev/).

```bash
mise install          # Install Go, task, golangci-lint
task build            # Build
task test             # Run tests (requires Docker)
task lint             # Lint
task fmt              # Format
```

## License

MIT
