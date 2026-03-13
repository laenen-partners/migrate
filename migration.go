package migrate

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Migration represents a single parsed migration file.
type Migration struct {
	Version string
	Name    string
	UpSQL   string
	DownSQL string
}

func parseMigrations(fsys fs.FS) ([]Migration, error) {
	var migrations []Migration

	entries, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("globbing migrations: %w", err)
	}

	for _, entry := range entries {
		data, err := fs.ReadFile(fsys, entry)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", entry, err)
		}

		base := filepath.Base(entry)
		name := strings.TrimSuffix(base, ".sql")

		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration filename %q: expected TIMESTAMP_description.sql", base)
		}
		version := parts[0]
		description := parts[1]

		upSQL, downSQL, err := parseSections(string(data), base)
		if err != nil {
			return nil, err
		}

		migrations = append(migrations, Migration{
			Version: version,
			Name:    description,
			UpSQL:   upSQL,
			DownSQL: downSQL,
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func parseSections(content, filename string) (upSQL, downSQL string, err error) {
	const (
		upMarker   = "-- migrate:up"
		downMarker = "-- migrate:down"
	)

	upIdx := strings.Index(content, upMarker)
	downIdx := strings.Index(content, downMarker)

	if upIdx == -1 {
		return "", "", fmt.Errorf("%s: missing %q section", filename, upMarker)
	}

	if downIdx == -1 {
		return "", "", fmt.Errorf("%s: missing %q section", filename, downMarker)
	}

	if upIdx > downIdx {
		return "", "", fmt.Errorf("%s: %q must appear before %q", filename, upMarker, downMarker)
	}

	upStart := upIdx + len(upMarker)
	upSQL = strings.TrimSpace(content[upStart:downIdx])

	downStart := downIdx + len(downMarker)
	downSQL = strings.TrimSpace(content[downStart:])

	return upSQL, downSQL, nil
}
