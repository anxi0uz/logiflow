package services

import (
	"context"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
)

func TestExpirySkipsBusyResources(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	for i, table := range []string{"orders", "assignments", "drivers", "vehicles"} {
		t.Run(table, func(t *testing.T) {
			busy := newExpiryFixture(t, pool, 30+i*2)
			independent := newExpiryFixture(t, pool, 31+i*2)
			busy.makeExpired(t)
			independent.makeExpired(t)
			resourceID := map[string]uuid.UUID{
				"orders": busy.order.ID, "assignments": busy.offer.ID,
				"drivers": busy.driver.ID, "vehicles": busy.vehicle.ID,
			}[table]
			queries := map[string]string{
				"orders":      "SELECT id FROM orders WHERE id = $1 FOR UPDATE",
				"assignments": "SELECT id FROM assignments WHERE id = $1 FOR UPDATE",
				"drivers":     "SELECT id FROM drivers WHERE id = $1 FOR UPDATE",
				"vehicles":    "SELECT id FROM vehicles WHERE id = $1 FOR UPDATE",
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck
			if _, err := tx.Exec(ctx, queries[table], resourceID); err != nil {
				t.Fatal(err)
			}
			sweepCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if err := busy.service.expireDueAssignments(sweepCtx); err != nil {
				t.Fatalf("busy %s stopped the sweep: %v", table, err)
			}
			assertOfferStatus(t, busy, models.AssignmentPendingAcceptance)
			assertOfferStatus(t, independent, models.AssignmentExpired)
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err := busy.service.expireDueAssignments(ctx); err != nil {
				t.Fatal(err)
			}
			assertOfferStatus(t, busy, models.AssignmentExpired)
		})
	}
}

func TestDispatchReleasePagesAndCoalescing(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	// These tests use a throwaway database; discard pending jobs from other cases.
	if _, err := pool.Exec(ctx, `TRUNCATE dispatch_release_queue, integration_outbox`); err != nil {
		t.Fatal(err)
	}
	first := newExpiryFixture(t, pool, 50)
	second := newExpiryFixture(t, pool, 50)
	orders := []models.Order{first.order, second.order}
	for range 205 {
		orders = append(orders, createReadyOrder(t, ctx, pool, first.ownerID, first.warehouseID, first.from, first.to))
	}
	first.makeExpired(t)
	second.makeExpired(t)
	if err := first.service.expireDueAssignments(ctx); err != nil {
		t.Fatal(err)
	}
	for _, f := range []*expiryFixture{first, second} {
		if got := dispatchReleaseCount(t, pool, f.offer.ID); got != 1 {
			t.Fatalf("release records = %d, want 1", got)
		}
	}
	for _, order := range orders {
		if got := dispatchRequestCount(t, pool, order.ID); got != 0 {
			t.Fatalf("command expanded refresh for %s synchronously", order.ID)
		}
	}
	if processed, err := first.service.processDispatchRelease(ctx); err != nil || !processed {
		t.Fatalf("first page: processed=%v, err=%v", processed, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM integration_outbox WHERE subject = 'dispatch.requested.v1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != dispatchRefreshBatchSize {
		t.Fatalf("first page emitted %d triggers, want %d", count, dispatchRefreshBatchSize)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dispatch_release_queue WHERE last_order_id IS NOT NULL`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("page cursor was not committed")
	}
	// A fresh service resumes the persisted cursor after a worker restart.
	drainDispatchReleases(t, second.service)
	for _, order := range orders {
		if got := dispatchRequestCount(t, pool, order.ID); got != 1 {
			t.Fatalf("overlapping releases emitted %d triggers for %s, want 1", got, order.ID)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dispatch_release_queue`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("completed jobs remain queued")
	}
	order := orders[2]
	var previousID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM integration_outbox WHERE subject = 'dispatch.requested.v1' AND payload->>'order_id' = $1`, order.ID.String()).Scan(&previousID); err != nil {
		t.Fatal(err)
	}
	request := func() {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		if err := enqueueDispatchRequest(ctx, tx, &order); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	request()
	var currentID uuid.UUID
	var payloadID string
	if err := pool.QueryRow(ctx, `SELECT id, payload->>'event_id' FROM integration_outbox WHERE subject = 'dispatch.requested.v1' AND payload->>'order_id' = $1`, order.ID.String()).Scan(&currentID, &payloadID); err != nil {
		t.Fatal(err)
	}
	if currentID == previousID || payloadID != currentID.String() {
		t.Fatal("replacement must use a fresh, matching event ID for transport deduplication")
	}
	if got := dispatchRequestCount(t, pool, order.ID); got != 1 {
		t.Fatalf("pending replacement created %d rows", got)
	}
	if _, err := pool.Exec(ctx, `UPDATE integration_outbox SET published_at = NOW() WHERE id = $1`, currentID); err != nil {
		t.Fatal(err)
	}
	request()
	if got := dispatchRequestCount(t, pool, order.ID); got != 2 {
		t.Fatalf("new refresh after publication has %d rows, want 2", got)
	}
}

func TestExpiryAdvancesPastBusyPage(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	busy := newExpiryFixture(t, pool, 60)
	busy.makeExpired(t)
	ids := []uuid.UUID{busy.offer.ID}
	for i := 1; i < assignmentExpiryBatchSize; i++ {
		from := busy.from.Add(time.Duration(i) * 24 * time.Hour)
		order := createReadyOrder(t, ctx, pool, busy.ownerID, busy.warehouseID, from, from.Add(time.Hour))
		offer := *busy.offer
		offer.ID, offer.OrderID = uuid.New(), order.ID
		offer.PlannedFrom, offer.PlannedTo = from, from.Add(time.Hour)
		offer.AssignedAt, offer.OfferExpiresAt = time.Now().Add(-16*time.Minute), time.Now().Add(-time.Minute)
		if err := storage.Create(ctx, "assignments", offer, pool); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, offer.ID)
	}
	independent := newExpiryFixture(t, pool, 40)
	independent.makeExpired(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT id FROM drivers WHERE id = $1 FOR UPDATE`, busy.driver.ID); err != nil {
		t.Fatal(err)
	}
	var cursor *assignmentExpiryCursor
	for range 2 {
		if err := busy.service.expireDueAssignmentsAfter(ctx, &cursor); err != nil {
			t.Fatal(err)
		}
	}
	assertOfferStatus(t, independent, models.AssignmentExpired)
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM assignments WHERE id = ANY($1) AND status = 'pending_acceptance'`, ids).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != assignmentExpiryBatchSize {
		t.Fatalf("busy page has %d pending offers", pending)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := busy.service.expireDueAssignmentsAfter(ctx, &cursor); err != nil {
		t.Fatal(err)
	}
	assertOfferStatus(t, busy, models.AssignmentExpired)
}
