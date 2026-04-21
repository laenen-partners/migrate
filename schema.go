package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const createTableSQL = `
CREATE TABLE IF NOT EXISTS scoped_schema_migrations (
	scope      TEXT        NOT NULL,
	version    TEXT        NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (scope, version)
);`

func ensureTable(ctx context.Context, conn *pgxpool.Conn) error {
	// Use a fixed advisory lock to serialize table creation across connections.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(-1)`); err != nil {
		return fmt.Errorf("acquiring table creation lock: %w", err)
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock(-1)`) //nolint:errcheck // best-effort unlock

	_, err := conn.Exec(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("creating scoped_schema_migrations table: %w", err)
	}
	return nil
}

func appliedVersions(ctx context.Context, conn *pgxpool.Conn, scope string) (map[string]bool, error) {
	rows, err := conn.Query(ctx, `SELECT version FROM scoped_schema_migrations WHERE scope = $1`, scope)
	if err != nil {
		return nil, fmt.Errorf("querying applied versions: %w", err)
	}
	defer rows.Close()

	versions := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scanning version: %w", err)
		}
		versions[v] = true
	}
	return versions, rows.Err()
}

func appliedVersionsOrdered(ctx context.Context, conn *pgxpool.Conn, scope string) ([]string, error) {
	rows, err := conn.Query(ctx,
		`SELECT version FROM scoped_schema_migrations WHERE scope = $1 ORDER BY version DESC`, scope)
	if err != nil {
		return nil, fmt.Errorf("querying applied versions: %w", err)
	}
	defer rows.Close()

	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scanning version: %w", err)
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func recordVersion(ctx context.Context, tx pgx.Tx, scope, version string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO scoped_schema_migrations (scope, version) VALUES ($1, $2)`, scope, version)
	if err != nil {
		return fmt.Errorf("recording migration %s: %w", version, err)
	}
	return nil
}

func removeVersion(ctx context.Context, tx pgx.Tx, scope, version string) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM scoped_schema_migrations WHERE scope = $1 AND version = $2`, scope, version)
	if err != nil {
		return fmt.Errorf("removing migration %s: %w", version, err)
	}
	return nil
}

func acquireAdvisoryLock(ctx context.Context, conn *pgxpool.Conn, scope string) error {
	_, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext($1))`, scope)
	if err != nil {
		return fmt.Errorf("acquiring advisory lock for scope %q: %w", scope, err)
	}
	return nil
}

func releaseAdvisoryLock(ctx context.Context, conn *pgxpool.Conn, scope string) error {
	_, err := conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, scope)
	if err != nil {
		return fmt.Errorf("releasing advisory lock for scope %q: %w", scope, err)
	}
	return nil
}
