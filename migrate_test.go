package migrate_test

import (
	"context"
	"fmt"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laenen-partners/migrate"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("terminating postgres container: %v", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"20240101120000_create_users.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

-- migrate:down
DROP TABLE users;
`),
		},
		"20240102120000_add_email.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
ALTER TABLE users ADD COLUMN email TEXT;

-- migrate:down
ALTER TABLE users DROP COLUMN email;
`),
		},
	}
}

func TestUpAppliesAllMigrations(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	err := migrate.Up(ctx, pool, testFS(), "app")
	if err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Verify both migrations applied
	var count int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'app'`).Scan(&count)
	if err != nil {
		t.Fatalf("querying scoped_schema_migrations: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 applied migrations, got %d", count)
	}

	// Verify the table and column exist
	_, err = pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ('test', 'test@example.com')`)
	if err != nil {
		t.Fatalf("inserting into users: %v", err)
	}
}

func TestUpIsIdempotent(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	if err := migrate.Up(ctx, pool, testFS(), "app"); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	if err := migrate.Up(ctx, pool, testFS(), "app"); err != nil {
		t.Fatalf("second Up: %v", err)
	}

	var count int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'app'`).Scan(&count)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 applied migrations, got %d", count)
	}
}

func TestDownRollsBackSteps(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	if err := migrate.Up(ctx, pool, testFS(), "app"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Roll back 1 step (the most recent migration)
	if err := migrate.Down(ctx, pool, testFS(), "app", 1); err != nil {
		t.Fatalf("Down(1): %v", err)
	}

	var count int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'app'`).Scan(&count)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 applied migration after rollback, got %d", count)
	}

	// Verify email column was removed
	var hasEmail bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'users' AND column_name = 'email'
		)
	`).Scan(&hasEmail)
	if err != nil {
		t.Fatalf("checking column: %v", err)
	}
	if hasEmail {
		t.Error("email column should have been removed")
	}
}

func TestDownRollsBackAll(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	if err := migrate.Up(ctx, pool, testFS(), "app"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	if err := migrate.Down(ctx, pool, testFS(), "app", 0); err != nil {
		t.Fatalf("Down(0): %v", err)
	}

	var count int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'app'`).Scan(&count)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 applied migrations, got %d", count)
	}

	// Verify users table was dropped
	var exists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables WHERE table_name = 'users'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("checking table: %v", err)
	}
	if exists {
		t.Error("users table should have been dropped")
	}
}

func TestScopeIsolation(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	fsA := fstest.MapFS{
		"20240101120000_create_items.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
CREATE TABLE items (id SERIAL PRIMARY KEY);

-- migrate:down
DROP TABLE items;
`),
		},
	}

	fsB := fstest.MapFS{
		"20240101120000_create_orders.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
CREATE TABLE orders (id SERIAL PRIMARY KEY);

-- migrate:down
DROP TABLE orders;
`),
		},
	}

	if err := migrate.Up(ctx, pool, fsA, "scope_a"); err != nil {
		t.Fatalf("Up scope_a: %v", err)
	}
	if err := migrate.Up(ctx, pool, fsB, "scope_b"); err != nil {
		t.Fatalf("Up scope_b: %v", err)
	}

	// Rolling back scope_a should not affect scope_b
	if err := migrate.Down(ctx, pool, fsA, "scope_a", 0); err != nil {
		t.Fatalf("Down scope_a: %v", err)
	}

	var countA, countB int
	pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'scope_a'`).Scan(&countA)
	pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'scope_b'`).Scan(&countB)

	if countA != 0 {
		t.Errorf("scope_a: expected 0, got %d", countA)
	}
	if countB != 1 {
		t.Errorf("scope_b: expected 1, got %d", countB)
	}

	// Verify orders table still exists
	var exists bool
	pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'orders')`).Scan(&exists)
	if !exists {
		t.Error("orders table should still exist")
	}
}

func TestConcurrentMigrations(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	fs := fstest.MapFS{
		"20240101120000_create_widgets.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
CREATE TABLE IF NOT EXISTS widgets (id SERIAL PRIMARY KEY);

-- migrate:down
DROP TABLE IF EXISTS widgets;
`),
		},
	}

	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			errs <- migrate.Up(ctx, pool, fs, "concurrent")
		}()
	}

	for i := 0; i < 5; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Up: %v", err)
		}
	}

	var count int
	pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'concurrent'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 migration, got %d", count)
	}
}

func TestUpWithNoMigrations(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	emptyFS := fstest.MapFS{}
	if err := migrate.Up(ctx, pool, emptyFS, "empty"); err != nil {
		t.Fatalf("Up with empty FS: %v", err)
	}
}

func TestDownWithNoAppliedMigrations(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	if err := migrate.Down(ctx, pool, testFS(), "empty", 0); err != nil {
		t.Fatalf("Down with no applied: %v", err)
	}
}

func TestUpThenDownThenUp(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()
	fs := testFS()

	if err := migrate.Up(ctx, pool, fs, "roundtrip"); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	if err := migrate.Down(ctx, pool, fs, "roundtrip", 0); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if err := migrate.Up(ctx, pool, fs, "roundtrip"); err != nil {
		t.Fatalf("second Up: %v", err)
	}

	var count int
	pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'roundtrip'`).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}

	// Verify table works
	_, err := pool.Exec(ctx, `INSERT INTO users (name, email) VALUES ('roundtrip', 'rt@test.com')`)
	if err != nil {
		t.Fatalf("inserting after re-Up: %v", err)
	}
}

func TestMigrationOrder(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	// Create migrations that depend on order
	fs := fstest.MapFS{
		"20240101000000_first.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
CREATE TABLE ordered_test (id SERIAL PRIMARY KEY, step INT NOT NULL);
INSERT INTO ordered_test (step) VALUES (1);

-- migrate:down
DROP TABLE ordered_test;
`),
		},
		"20240102000000_second.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
INSERT INTO ordered_test (step) VALUES (2);

-- migrate:down
DELETE FROM ordered_test WHERE step = 2;
`),
		},
		"20240103000000_third.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
INSERT INTO ordered_test (step) VALUES (3);

-- migrate:down
DELETE FROM ordered_test WHERE step = 3;
`),
		},
	}

	if err := migrate.Up(ctx, pool, fs, "order"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	rows, err := pool.Query(ctx, `SELECT step FROM ordered_test ORDER BY id`)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	defer rows.Close()

	var steps []int
	for rows.Next() {
		var s int
		rows.Scan(&s)
		steps = append(steps, s)
	}

	if len(steps) != 3 || steps[0] != 1 || steps[1] != 2 || steps[2] != 3 {
		t.Errorf("expected [1,2,3], got %v", steps)
	}

	// Roll back 2 steps
	if err := migrate.Down(ctx, pool, fs, "order", 2); err != nil {
		t.Fatalf("Down(2): %v", err)
	}

	var remaining int
	pool.QueryRow(ctx, `SELECT count(*) FROM ordered_test`).Scan(&remaining)
	if remaining != 1 {
		t.Errorf("expected 1 row remaining, got %d", remaining)
	}

	var applied int
	pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'order'`).Scan(&applied)
	if applied != 1 {
		t.Errorf("expected 1 applied migration, got %d", applied)
	}
}

func TestPartialUpOnError(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	fs := fstest.MapFS{
		"20240101000000_good.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
CREATE TABLE partial_test (id SERIAL PRIMARY KEY);

-- migrate:down
DROP TABLE partial_test;
`),
		},
		"20240102000000_bad.sql": &fstest.MapFile{
			Data: []byte(`-- migrate:up
THIS IS NOT VALID SQL;

-- migrate:down
SELECT 1;
`),
		},
	}

	err := migrate.Up(ctx, pool, fs, "partial")
	if err == nil {
		t.Fatal("expected error from bad migration")
	}

	// First migration should have been applied
	var count int
	pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'partial'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 applied migration (good one), got %d", count)
	}

	// Table from first migration should exist
	var exists bool
	pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'partial_test')`).Scan(&exists)
	if !exists {
		t.Error("partial_test table should exist from first migration")
	}
}

func TestManyMigrations(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	fs := make(fstest.MapFS)
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("2024010112%04d_migration_%d.sql", i, i)
		fs[name] = &fstest.MapFile{
			Data: []byte(fmt.Sprintf(`-- migrate:up
CREATE TABLE batch_table_%d (id SERIAL PRIMARY KEY);

-- migrate:down
DROP TABLE batch_table_%d;
`, i, i)),
		}
	}

	if err := migrate.Up(ctx, pool, fs, "batch"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	var count int
	pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'batch'`).Scan(&count)
	if count != 20 {
		t.Errorf("expected 20 migrations, got %d", count)
	}

	if err := migrate.Down(ctx, pool, fs, "batch", 10); err != nil {
		t.Fatalf("Down(10): %v", err)
	}

	pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'batch'`).Scan(&count)
	if count != 10 {
		t.Errorf("expected 10 migrations remaining, got %d", count)
	}
}
