// Package migrate provides a simple, library-first PostgreSQL migration tool
// with scoped migrations. Multiple modules can manage their own migrations
// independently using the same database.
package migrate

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Up applies all pending migrations from fsys for the given scope.
func Up(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, scope string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := ensureTable(ctx, conn); err != nil {
		return err
	}

	if err := acquireAdvisoryLock(ctx, conn, scope); err != nil {
		return err
	}
	defer releaseAdvisoryLock(ctx, conn, scope)

	migrations, err := parseMigrations(fsys)
	if err != nil {
		return err
	}

	applied, err := appliedVersions(ctx, conn, scope)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("beginning transaction for %s: %w", m.Version, err)
		}

		if _, err := tx.Exec(ctx, m.UpSQL); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("executing up migration %s (%s): %w", m.Version, m.Name, err)
		}

		if err := recordVersion(ctx, tx, scope, m.Version); err != nil {
			tx.Rollback(ctx)
			return err
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing migration %s: %w", m.Version, err)
		}
	}

	return nil
}

// Down rolls back the most recent `steps` migrations for the given scope.
// If steps is 0, all applied migrations are rolled back.
func Down(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, scope string, steps int) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := ensureTable(ctx, conn); err != nil {
		return err
	}

	if err := acquireAdvisoryLock(ctx, conn, scope); err != nil {
		return err
	}
	defer releaseAdvisoryLock(ctx, conn, scope)

	migrations, err := parseMigrations(fsys)
	if err != nil {
		return err
	}

	migrationsByVersion := make(map[string]Migration, len(migrations))
	for _, m := range migrations {
		migrationsByVersion[m.Version] = m
	}

	applied, err := appliedVersionsOrdered(ctx, conn, scope)
	if err != nil {
		return err
	}

	if steps > 0 && steps < len(applied) {
		applied = applied[:steps]
	}

	for _, version := range applied {
		m, ok := migrationsByVersion[version]
		if !ok {
			return fmt.Errorf("migration file for version %s not found", version)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("beginning transaction for rollback %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, m.DownSQL); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("executing down migration %s (%s): %w", m.Version, m.Name, err)
		}

		if err := removeVersion(ctx, tx, scope, version); err != nil {
			tx.Rollback(ctx)
			return err
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing rollback %s: %w", version, err)
		}
	}

	return nil
}
