package migrate_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/laenen-partners/migrate"
)

func TestUpGoAppliesMigrations(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	called := make([]string, 0, 2)
	migrations := []migrate.GoMigration{
		{
			Version: "20240101120000",
			Name:    "first",
			Up:      func(ctx context.Context) error { called = append(called, "first"); return nil },
		},
		{
			Version: "20240102120000",
			Name:    "second",
			Up:      func(ctx context.Context) error { called = append(called, "second"); return nil },
		},
	}

	if err := migrate.UpGo(ctx, pool, migrations, "go-app"); err != nil {
		t.Fatalf("UpGo: %v", err)
	}

	if len(called) != 2 || called[0] != "first" || called[1] != "second" {
		t.Errorf("expected [first, second], got %v", called)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'go-app'`).Scan(&count); err != nil {
		t.Fatalf("querying: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 applied migrations, got %d", count)
	}
}

func TestUpGoIsIdempotent(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	callCount := 0
	migrations := []migrate.GoMigration{
		{
			Version: "20240101120000",
			Name:    "only",
			Up:      func(ctx context.Context) error { callCount++; return nil },
		},
	}

	if err := migrate.UpGo(ctx, pool, migrations, "go-idem"); err != nil {
		t.Fatalf("first UpGo: %v", err)
	}
	if err := migrate.UpGo(ctx, pool, migrations, "go-idem"); err != nil {
		t.Fatalf("second UpGo: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected Up called once, got %d", callCount)
	}
}

func TestUpGoSortsbyVersion(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	var order []string
	migrations := []migrate.GoMigration{
		{
			Version: "20240103120000",
			Name:    "third",
			Up:      func(ctx context.Context) error { order = append(order, "third"); return nil },
		},
		{
			Version: "20240101120000",
			Name:    "first",
			Up:      func(ctx context.Context) error { order = append(order, "first"); return nil },
		},
		{
			Version: "20240102120000",
			Name:    "second",
			Up:      func(ctx context.Context) error { order = append(order, "second"); return nil },
		},
	}

	if err := migrate.UpGo(ctx, pool, migrations, "go-sort"); err != nil {
		t.Fatalf("UpGo: %v", err)
	}

	if len(order) != 3 || order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Errorf("expected [first, second, third], got %v", order)
	}
}

func TestUpGoStopsOnError(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	migrations := []migrate.GoMigration{
		{
			Version: "20240101120000",
			Name:    "good",
			Up:      func(ctx context.Context) error { return nil },
		},
		{
			Version: "20240102120000",
			Name:    "bad",
			Up:      func(ctx context.Context) error { return fmt.Errorf("something went wrong") },
		},
	}

	err := migrate.UpGo(ctx, pool, migrations, "go-partial")
	if err == nil {
		t.Fatal("expected error from bad migration")
	}

	var count int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'go-partial'`).Scan(&count)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 applied migration (good one), got %d", count)
	}
}

func TestUpGoValidatesEmptyVersion(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	migrations := []migrate.GoMigration{
		{Version: "", Name: "bad", Up: func(ctx context.Context) error { return nil }},
	}
	if err := migrate.UpGo(ctx, pool, migrations, "go-val"); err == nil {
		t.Fatal("expected error for empty version")
	}
}

func TestUpGoValidatesVersionFormat(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	migrations := []migrate.GoMigration{
		{Version: "not-a-timestamp", Name: "bad", Up: func(ctx context.Context) error { return nil }},
	}
	if err := migrate.UpGo(ctx, pool, migrations, "go-val"); err == nil {
		t.Fatal("expected error for invalid version format")
	}
}

func TestUpGoValidatesEmptyName(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	migrations := []migrate.GoMigration{
		{Version: "20240101120000", Name: "", Up: func(ctx context.Context) error { return nil }},
	}
	if err := migrate.UpGo(ctx, pool, migrations, "go-val"); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestUpGoValidatesNilUp(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	migrations := []migrate.GoMigration{
		{Version: "20240101120000", Name: "bad", Up: nil},
	}
	if err := migrate.UpGo(ctx, pool, migrations, "go-val"); err == nil {
		t.Fatal("expected error for nil Up")
	}
}

func TestUpGoValidatesDuplicateVersion(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	migrations := []migrate.GoMigration{
		{Version: "20240101120000", Name: "first", Up: func(ctx context.Context) error { return nil }},
		{Version: "20240101120000", Name: "dupe", Up: func(ctx context.Context) error { return nil }},
	}
	if err := migrate.UpGo(ctx, pool, migrations, "go-val"); err == nil {
		t.Fatal("expected error for duplicate version")
	}
}

func TestUpGoWithEmptySlice(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	if err := migrate.UpGo(ctx, pool, nil, "go-empty"); err != nil {
		t.Fatalf("UpGo with nil slice: %v", err)
	}
	if err := migrate.UpGo(ctx, pool, []migrate.GoMigration{}, "go-empty"); err != nil {
		t.Fatalf("UpGo with empty slice: %v", err)
	}
}

func TestUpGoScopeIsolation(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	migA := []migrate.GoMigration{
		{Version: "20240101120000", Name: "a", Up: func(ctx context.Context) error { return nil }},
	}
	migB := []migrate.GoMigration{
		{Version: "20240101120000", Name: "b", Up: func(ctx context.Context) error { return nil }},
	}

	if err := migrate.UpGo(ctx, pool, migA, "go-scope-a"); err != nil {
		t.Fatalf("UpGo scope-a: %v", err)
	}
	if err := migrate.UpGo(ctx, pool, migB, "go-scope-b"); err != nil {
		t.Fatalf("UpGo scope-b: %v", err)
	}

	var countA, countB int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'go-scope-a'`).Scan(&countA); err != nil {
		t.Fatalf("querying scope-a: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scoped_schema_migrations WHERE scope = 'go-scope-b'`).Scan(&countB); err != nil {
		t.Fatalf("querying scope-b: %v", err)
	}

	if countA != 1 {
		t.Errorf("scope-a: expected 1, got %d", countA)
	}
	if countB != 1 {
		t.Errorf("scope-b: expected 1, got %d", countB)
	}
}
