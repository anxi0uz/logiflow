package services

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func TestDemoSeedAtomicRepeatableAndOperable(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	clear := func() {
		if _, err := pool.Exec(ctx, `TRUNCATE integration_outbox,integration_inbox,dispatch_release_queue,users,warehouses,vehicles CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	clear()
	t.Cleanup(clear)
	service := NewOrderService(pool, config.Config{})
	password := "demo-test-password-123"
	// A collision must roll back all the inserts preceding it, without deleting
	// the existing account. This is not an upsert of someone else's data.
	foreignID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,slug,password_hash,role) VALUES($1,'manager.kazan@demo.logiflow.test','existing-account','existing-hash','client')`, foreignID); err != nil {
		t.Fatal(err)
	}
	if created, err := service.SeedDemo(ctx, password); err == nil || created {
		t.Fatalf("collision accepted: %v %v", created, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("not atomic: %d %v", count, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET email='existing@demo.logiflow.test' WHERE id=$1`, foreignID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	createdCount := 0
	for range 2 {
		wg.Go(func() {
			created, err := service.SeedDemo(ctx, password)
			if err != nil {
				t.Errorf("concurrent seed: %v", err)
				return
			}
			if created {
				mu.Lock()
				createdCount++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if createdCount != 1 {
		t.Fatalf("created %d copies", createdCount)
	}
	for table, want := range map[string]int{"users": 13, "warehouses": 3, "vehicles": 6, "drivers": 6, "counterparties": 7, "orders": 9, "assignments": 7, "trip_handovers": 1, "delivery_receipts": 3, "order_incidents": 1} {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s: want %d, got %d: %v", table, want, count, err)
		}
	}
	var hash string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, demoID("user/admin")).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		t.Fatal(err)
	}
	if created, err := service.SeedDemo(ctx, "different-password-456"); err != nil || created {
		t.Fatalf("repeat changed data: %v %v", created, err)
	}
	var unchanged string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, demoID("user/admin")).Scan(&unchanged); err != nil || unchanged != hash {
		t.Fatal("repeat reset password", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_outbox WHERE subject IN ('document.requested.v1','delivery.completed.v1')`).Scan(&count); err != nil || count != 19 {
		t.Fatalf("document snapshots: %d %v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_outbox WHERE subject IN ('document.requested.v1','delivery.completed.v1') AND payload->'recipients' @> jsonb_build_array($1::text)`, demoID("user/admin").String()).Scan(&count); err != nil || count != 19 {
		t.Fatalf("demo admin document access: %d %v", count, err)
	}
	// Seeded accepted assignments participate in the real commands, not just lists.
	assignmentID, driverID := demoID("assignment/4"), demoID("user/driver2")
	if _, err := service.StartAssignment(ctx, assignmentID, driverID, "driver"); !errors.Is(err, ErrWarehouseOperationRequired) {
		t.Fatalf("missing load accepted: %v", err)
	}
	if _, err := service.CreateWarehouseOperation(ctx, demoID("order/4"), demoID("user/manager.moscow"), "manager", api.WarehouseOperationCreate{OperationType: "load", AssignmentId: &assignmentID, PackageCount: 10, CargoCondition: "Упаковка целая"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartAssignment(ctx, assignmentID, driverID, "driver"); err != nil {
		t.Fatal(err)
	}
	if created, err := service.SeedDemo(ctx, password); err != nil || created {
		t.Fatal("repeat after progress", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, demoID("order/4")).Scan(&state); err != nil || state != "in_transit" {
		t.Fatalf("progress reset: %s %v", state, err)
	}
}
