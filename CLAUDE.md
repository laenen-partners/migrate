# migrate

Go library for scoped PostgreSQL migrations using pgx v5.

## Build & Test

```bash
task build        # go build ./...
task test         # go test -v -count=1 ./...
task lint         # golangci-lint run
```

Or directly:
```bash
go build ./...
go test -v -count=1 ./...
```

## Architecture

- `migrate.go` — Public API: `Up()` and `Down()`
- `migration.go` — Parse migration SQL files from `fs.FS`
- `schema.go` — Database operations: table management, version tracking, advisory locks

## Conventions

- Migration files: `YYYYMMDDHHMMSS_description.sql` with `-- migrate:up` and `-- migrate:down` sections
- Table name: `scoped_schema_migrations` (scope + version composite PK)
- Tests use testcontainers-go with `postgres:16-alpine`
- Each test gets its own Postgres container via `setupPostgres(t)`
