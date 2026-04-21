package migrate

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StatusResult describes the migration state for a given scope.
type StatusResult struct {
	Scope    string
	Total    int
	Applied  int
	Pending  int
	Versions []VersionStatus
}

// VersionStatus describes whether a single migration version has been applied.
type VersionStatus struct {
	Version string
	Name    string
	Applied bool
}

// Status returns the migration state for the given scope by comparing
// the migrations on disk with the versions recorded in the database.
func Status(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, scope string) (StatusResult, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return StatusResult{}, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := ensureTable(ctx, conn); err != nil {
		return StatusResult{}, err
	}

	migrations, err := parseMigrations(fsys)
	if err != nil {
		return StatusResult{}, err
	}

	applied, err := appliedVersions(ctx, conn, scope)
	if err != nil {
		return StatusResult{}, err
	}

	result := StatusResult{
		Scope:    scope,
		Total:    len(migrations),
		Versions: make([]VersionStatus, len(migrations)),
	}

	for i, m := range migrations {
		isApplied := applied[m.Version]
		result.Versions[i] = VersionStatus{
			Version: m.Version,
			Name:    m.Name,
			Applied: isApplied,
		}
		if isApplied {
			result.Applied++
		}
	}
	result.Pending = result.Total - result.Applied

	return result, nil
}
