package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func expiryTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("LOGIFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOGIFLOW_TEST_DATABASE_URL is not set")
	}
	t.Chdir("../..")
	if err := database.RunMigrations(context.Background(), databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type expiryFixture struct {
	pool                               *pgxpool.Pool
	service                            *OrderService
	ownerID, driverUserID, warehouseID uuid.UUID
	driver                             models.Driver
	vehicle                            models.Vehicle
	order                              models.Order
	offer                              *models.Assignment
	from, to                           time.Time
}

func newExpiryFixture(t *testing.T, pool *pgxpool.Pool, day int) *expiryFixture {
	t.Helper()
	ctx := context.Background()
	f := &expiryFixture{pool: pool, service: NewOrderService(pool, config.Config{}), ownerID: uuid.New(), driverUserID: uuid.New(), warehouseID: uuid.New()}
	for _, user := range []models.User{
		{ID: f.ownerID, Role: "client"}, {ID: f.driverUserID, Role: "driver"},
	} {
		user.Email, user.Slug, user.FullName, user.PasswordHash = user.ID.String()+"@expiry.test", user.ID.String(), "Expiry test", "test"
		user.CreatedAt = time.Now()
		if err := storage.Create(ctx, "users", user, pool); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.Create(ctx, "warehouses", models.Warehouse{ID: f.warehouseID, Name: "Expiry", Slug: f.warehouseID.String(), Address: "Origin", City: "Test", CreatedAt: time.Now()}, pool); err != nil {
		t.Fatal(err)
	}
	f.driver = models.Driver{ID: uuid.New(), UserID: f.driverUserID, LicenseNumber: uuid.NewString(), LicenseExpiry: time.Now().AddDate(1, 0, 0), Rating: 5, Status: "available", Slug: uuid.NewString()}
	f.vehicle = models.Vehicle{ID: uuid.New(), PlateNumber: uuid.NewString()[:18], CapacityKg: 5000, CapacityM3: 30, Status: "available", Slug: uuid.NewString()}
	if err := storage.Create(ctx, "drivers", f.driver, pool); err != nil {
		t.Fatal(err)
	}
	if err := storage.Create(ctx, "vehicles", f.vehicle, pool); err != nil {
		t.Fatal(err)
	}
	if err := storage.Create(ctx, "vehicle_documents", models.VehicleDocument{ID: uuid.New(), VehicleID: f.vehicle.ID, Type: "registration", Number: "TEST", ValidUntil: time.Now().AddDate(1, 0, 0), Status: "valid", CreatedAt: time.Now()}, pool); err != nil {
		t.Fatal(err)
	}
	f.from = time.Now().Add(time.Duration(day)*24*time.Hour + 2*time.Hour).Truncate(time.Second)
	f.to = f.from.Add(time.Hour)
	f.order = createReadyOrder(t, ctx, pool, f.ownerID, f.warehouseID, f.from, f.to)
	var err error
	f.offer, err = f.service.CreateAssignment(ctx, f.order.ID, f.ownerID, "admin", api.AssignmentCreate{DriverId: f.driver.ID, VehicleId: f.vehicle.ID, PlannedFrom: f.from, PlannedTo: f.to})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *expiryFixture) makeExpired(t *testing.T) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE assignments SET assigned_at = NOW() - INTERVAL '16 minutes', offer_expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, f.offer.ID); err != nil {
		t.Fatal(err)
	}
}

func dispatchRequestCount(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM integration_outbox WHERE subject = 'dispatch.requested.v1' AND payload->>'order_id' = $1`, orderID.String()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func dispatchReleaseCount(t *testing.T, pool *pgxpool.Pool, assignmentID uuid.UUID) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM dispatch_release_queue WHERE assignment_id = $1`, assignmentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func drainDispatchReleases(t *testing.T, service *OrderService) {
	t.Helper()
	for range 1000 {
		processed, err := service.processDispatchRelease(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !processed {
			return
		}
	}
	t.Fatal("release queue did not drain")
}

func assertOfferStatus(t *testing.T, f *expiryFixture, expected string) {
	t.Helper()
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM assignments WHERE id = $1`, f.offer.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != expected {
		t.Fatalf("offer status = %s, want %s", status, expected)
	}
}

func TestDispatchRefreshAfterReservationRelease(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	for day, command := range []string{"reject", "expiry", "late_accept", "lazy_expiry", "cancel", "reassign", "complete"} {
		t.Run(command, func(t *testing.T) {
			f := newExpiryFixture(t, pool, day)
			waiting := createReadyOrder(t, ctx, pool, f.ownerID, f.warehouseID, f.from, f.to)
			adjacent := createReadyOrder(t, ctx, pool, f.ownerID, f.warehouseID, f.to, f.to.Add(time.Hour))
			draft := createDraftOrder(t, ctx, pool, f.ownerID, f.warehouseID, f.from, f.to)
			expected, ownCount := models.AssignmentExpired, 1
			var err error
			switch command {
			case "reject":
				expected = models.AssignmentRejected
				_, err = f.service.RejectAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.AssignmentReject{ReasonCode: "unavailable"})
			case "expiry":
				f.makeExpired(t)
				err = f.service.expireDueAssignments(ctx)
			case "late_accept":
				f.makeExpired(t)
				_, err = f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver")
				if errors.Is(err, ErrAssignmentExpired) {
					err = nil
				}
			case "lazy_expiry":
				f.makeExpired(t)
				err = f.service.expireStaleAssignments(ctx, f.order.ID, f.driver.ID, f.vehicle.ID)
			case "cancel":
				expected, ownCount = models.AssignmentReleased, 0
				_, err = f.service.CancelOrder(ctx, f.order.ID, f.ownerID, "admin", api.OrderCancel{})
			case "reassign":
				expected, ownCount = models.AssignmentReleased, 0
				// Move the replacement window to leave the old reservation window free.
				_, err = f.service.CreateAssignment(ctx, f.order.ID, f.ownerID, "admin", api.AssignmentCreate{DriverId: f.driver.ID, VehicleId: f.vehicle.ID, PlannedFrom: f.to, PlannedTo: f.to.Add(time.Hour), ReplacesAssignmentId: &f.offer.ID})
			case "complete":
				expected, ownCount = models.AssignmentCompleted, 0
				if _, err = f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
					t.Fatal(err)
				}
				if _, err = f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
					t.Fatal(err)
				}
				if _, err = f.service.ArriveAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
					t.Fatal(err)
				}
				_, err = f.service.CompleteAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.DeliveryComplete{})
			}
			if err != nil {
				t.Fatal(err)
			}
			assertOfferStatus(t, f, expected)
			if got := dispatchReleaseCount(t, pool, f.offer.ID); got != 1 {
				t.Fatalf("release records = %d, want 1", got)
			}
			if got := dispatchRequestCount(t, pool, waiting.ID); got != 0 {
				t.Fatalf("HTTP command expanded releases synchronously: %d", got)
			}
			drainDispatchReleases(t, f.service)
			for id, want := range map[uuid.UUID]int{f.order.ID: ownCount, waiting.ID: 1, adjacent.ID: 0, draft.ID: 0} {
				if got := dispatchRequestCount(t, pool, id); got != want {
					t.Fatalf("refresh count for %s = %d, want %d", id, got, want)
				}
			}
			var payload []byte
			var eventID uuid.UUID
			if err := pool.QueryRow(ctx, `SELECT id, payload FROM integration_outbox WHERE subject = 'dispatch.requested.v1' AND payload->>'order_id' = $1`, waiting.ID.String()).Scan(&eventID, &payload); err != nil {
				t.Fatal(err)
			}
			var request events.DispatchRequest
			if err := json.Unmarshal(payload, &request); err != nil {
				t.Fatal(err)
			}
			if request.EventID != eventID || request.OrderID != waiting.ID || !request.SubmittedAt.Equal(waiting.SubmittedAt.Truncate(time.Microsecond)) || request.RequestedAt.IsZero() {
				t.Fatalf("invalid refresh trigger: %+v", request)
			}
		})
	}
}

func TestAssignmentExpiryWorkerAndConcurrentCommands(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	t.Run("startup_periodic_sweep_and_shutdown", func(t *testing.T) {
		f := newExpiryFixture(t, pool, 20)
		f.makeExpired(t)
		later := newExpiryFixture(t, pool, 24)
		workerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan struct{})
		go func() { defer close(done); f.service.RunAssignmentExpiry(workerCtx) }()
		deadline := time.Now().Add(5 * time.Second)
		for dispatchReleaseCount(t, pool, f.offer.ID) == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		// The second offer was valid during the startup scan. It must be closed
		// by a later tick without any driver or manager command.
		if dispatchReleaseCount(t, pool, f.offer.ID) != 1 {
			t.Fatal("startup sweep did not run")
		}
		later.makeExpired(t)
		deadline = time.Now().Add(assignmentExpiryInterval + 5*time.Second)
		for dispatchReleaseCount(t, pool, later.offer.ID) == 0 && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("expiry worker did not stop")
		}
		assertOfferStatus(t, f, models.AssignmentExpired)
		assertOfferStatus(t, later, models.AssignmentExpired)
		drainDispatchReleases(t, f.service)
		if got := dispatchRequestCount(t, pool, later.order.ID); got != 1 {
			t.Fatalf("periodic refresh count = %d", got)
		}
		if err := f.service.expireDueAssignments(ctx); err != nil {
			t.Fatal(err)
		}
		if got := dispatchRequestCount(t, pool, f.order.ID); got != 1 {
			t.Fatalf("expiry repeated refresh: %d", got)
		}
	})
	t.Run("concurrent_expiry_and_accept", func(t *testing.T) {
		f := newExpiryFixture(t, pool, 21)
		f.makeExpired(t)
		errs := make(chan error, 3)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() { errs <- f.service.expireDueAssignments(ctx) })
		}
		wg.Go(func() {
			_, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver")
			if errors.Is(err, ErrAssignmentExpired) || errors.Is(err, ErrAssignmentStale) {
				err = nil
			}
			errs <- err
		})
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		assertOfferStatus(t, f, models.AssignmentExpired)
		if got := dispatchReleaseCount(t, pool, f.offer.ID); got != 1 {
			t.Fatalf("concurrent release records = %d", got)
		}
		drainDispatchReleases(t, f.service)
		if got := dispatchRequestCount(t, pool, f.order.ID); got != 1 {
			t.Fatalf("concurrent refresh count = %d", got)
		}
		var notices int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND title = 'Предложение истекло'`, f.ownerID).Scan(&notices); err != nil {
			t.Fatal(err)
		}
		if notices != 1 {
			t.Fatalf("expiry notifications = %d", notices)
		}
	})
	t.Run("accepted_offer_survives_stale_scan", func(t *testing.T) {
		f := newExpiryFixture(t, pool, 22)
		if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
			t.Fatal(err)
		}
		f.makeExpired(t)
		if err := f.service.expireAssignments(ctx, []models.Assignment{*f.offer}); err != nil {
			t.Fatal(err)
		}
		assertOfferStatus(t, f, models.AssignmentAccepted)
		if got := dispatchRequestCount(t, pool, f.order.ID); got != 0 {
			t.Fatalf("accepted offer refresh count = %d", got)
		}
	})
	t.Run("rollback_keeps_offer_and_events", func(t *testing.T) {
		f := newExpiryFixture(t, pool, 23)
		f.makeExpired(t)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		_, offer, _, _, err := f.service.lockAssignmentContext(ctx, tx, f.offer.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.service.expireAssignment(ctx, tx, offer, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		assertOfferStatus(t, f, models.AssignmentPendingAcceptance)
		if got := dispatchReleaseCount(t, pool, f.offer.ID); got != 0 {
			t.Fatalf("rolled back release records = %d", got)
		}
		if got := dispatchRequestCount(t, pool, f.order.ID); got != 0 {
			t.Fatalf("rolled back refresh count = %d", got)
		}
		var notices int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND title = 'Предложение истекло'`, f.ownerID).Scan(&notices); err != nil {
			t.Fatal(err)
		}
		if notices != 0 {
			t.Fatalf("rolled back notifications = %d", notices)
		}
		if err := f.service.expireDueAssignments(ctx); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("bounded_batches", func(t *testing.T) {
		f := newExpiryFixture(t, pool, 100)
		f.makeExpired(t)
		ids := []uuid.UUID{f.offer.ID}
		for i := 1; i <= assignmentExpiryBatchSize; i++ {
			from := f.from.Add(time.Duration(i) * 24 * time.Hour)
			order := createReadyOrder(t, ctx, pool, f.ownerID, f.warehouseID, from, from.Add(time.Hour))
			offer := *f.offer
			offer.ID, offer.OrderID = uuid.New(), order.ID
			offer.PlannedFrom, offer.PlannedTo = from, from.Add(time.Hour)
			offer.AssignedAt, offer.OfferExpiresAt = time.Now().Add(-16*time.Minute), time.Now().Add(-time.Minute)
			if err := storage.Create(ctx, "assignments", offer, pool); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, offer.ID)
		}
		for _, want := range []int{1, 0} {
			if err := f.service.expireDueAssignments(ctx); err != nil {
				t.Fatal(err)
			}
			var remaining int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM assignments WHERE id = ANY($1) AND status = 'pending_acceptance'`, ids).Scan(&remaining); err != nil {
				t.Fatal(err)
			}
			if remaining != want {
				t.Fatalf("pending after batch = %d, want %d", remaining, want)
			}
		}
	})

}
