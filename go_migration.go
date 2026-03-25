package migrate

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GoMigration represents a single programmatic migration.
type GoMigration struct {
	Version string
	Name    string
	Up      func(ctx context.Context) error
}

var versionRe = regexp.MustCompile(`^\d{14}$`)

func validateGoMigrations(migrations []GoMigration) error {
	seen := make(map[string]bool, len(migrations))
	for _, m := range migrations {
		if m.Version == "" {
			return fmt.Errorf("go migration has empty version")
		}
		if !versionRe.MatchString(m.Version) {
			return fmt.Errorf("go migration %q: version must be 14 digits (YYYYMMDDHHMMSS)", m.Version)
		}
		if m.Name == "" {
			return fmt.Errorf("go migration %q: name is required", m.Version)
		}
		if m.Up == nil {
			return fmt.Errorf("go migration %s (%s): Up function is required", m.Version, m.Name)
		}
		if seen[m.Version] {
			return fmt.Errorf("go migration %q: duplicate version", m.Version)
		}
		seen[m.Version] = true
	}
	return nil
}

// UpGo applies all pending Go migrations for the given scope.
func UpGo(ctx context.Context, pool *pgxpool.Pool, migrations []GoMigration, scope string) error {
	if err := validateGoMigrations(migrations); err != nil {
		return err
	}

	sorted := make([]GoMigration, len(migrations))
	copy(sorted, migrations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Version < sorted[j].Version
	})

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

	applied, err := appliedVersions(ctx, conn, scope)
	if err != nil {
		return err
	}

	for _, m := range sorted {
		if applied[m.Version] {
			continue
		}

		if err := m.Up(ctx); err != nil {
			return fmt.Errorf("executing go migration %s (%s): %w", m.Version, m.Name, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("beginning transaction for version record %s: %w", m.Version, err)
		}

		if err := recordVersion(ctx, tx, scope, m.Version); err != nil {
			tx.Rollback(ctx)
			return err
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing version record %s: %w", m.Version, err)
		}
	}

	return nil
}
