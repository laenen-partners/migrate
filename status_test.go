package migrate_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/laenen-partners/migrate"
)

func TestStatusNoneApplied(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	result, err := migrate.Status(ctx, pool, testFS(), "app")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if result.Scope != "app" {
		t.Errorf("expected scope %q, got %q", "app", result.Scope)
	}
	if result.Total != 2 {
		t.Errorf("expected total 2, got %d", result.Total)
	}
	if result.Applied != 0 {
		t.Errorf("expected applied 0, got %d", result.Applied)
	}
	if result.Pending != 2 {
		t.Errorf("expected pending 2, got %d", result.Pending)
	}

	for _, v := range result.Versions {
		if v.Applied {
			t.Errorf("version %s should not be applied", v.Version)
		}
	}
}

func TestStatusAllApplied(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()
	fs := testFS()

	if err := migrate.Up(ctx, pool, fs, "app"); err != nil {
		t.Fatalf("Up: %v", err)
	}

	result, err := migrate.Status(ctx, pool, fs, "app")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if result.Total != 2 {
		t.Errorf("expected total 2, got %d", result.Total)
	}
	if result.Applied != 2 {
		t.Errorf("expected applied 2, got %d", result.Applied)
	}
	if result.Pending != 0 {
		t.Errorf("expected pending 0, got %d", result.Pending)
	}

	for _, v := range result.Versions {
		if !v.Applied {
			t.Errorf("version %s should be applied", v.Version)
		}
	}
}

func TestStatusPartiallyApplied(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()
	fs := testFS()

	if err := migrate.Up(ctx, pool, fs, "app"); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := migrate.Down(ctx, pool, fs, "app", 1); err != nil {
		t.Fatalf("Down(1): %v", err)
	}

	result, err := migrate.Status(ctx, pool, fs, "app")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if result.Applied != 1 {
		t.Errorf("expected applied 1, got %d", result.Applied)
	}
	if result.Pending != 1 {
		t.Errorf("expected pending 1, got %d", result.Pending)
	}

	if !result.Versions[0].Applied {
		t.Error("first version should be applied")
	}
	if result.Versions[1].Applied {
		t.Error("second version should not be applied")
	}
}

func TestStatusEmptyFS(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	result, err := migrate.Status(ctx, pool, fstest.MapFS{}, "empty")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if result.Total != 0 {
		t.Errorf("expected total 0, got %d", result.Total)
	}
	if result.Applied != 0 {
		t.Errorf("expected applied 0, got %d", result.Applied)
	}
	if result.Pending != 0 {
		t.Errorf("expected pending 0, got %d", result.Pending)
	}
	if len(result.Versions) != 0 {
		t.Errorf("expected empty versions, got %d", len(result.Versions))
	}
}

func TestStatusVersionOrder(t *testing.T) {
	pool := setupPostgres(t)
	ctx := context.Background()

	result, err := migrate.Status(ctx, pool, testFS(), "app")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if len(result.Versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(result.Versions))
	}
	if result.Versions[0].Version != "20240101120000" {
		t.Errorf("expected first version 20240101120000, got %s", result.Versions[0].Version)
	}
	if result.Versions[0].Name != "create_users" {
		t.Errorf("expected first name create_users, got %s", result.Versions[0].Name)
	}
	if result.Versions[1].Version != "20240102120000" {
		t.Errorf("expected second version 20240102120000, got %s", result.Versions[1].Version)
	}
	if result.Versions[1].Name != "add_email" {
		t.Errorf("expected second name add_email, got %s", result.Versions[1].Name)
	}
}
