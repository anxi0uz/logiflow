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
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCoreLifecycleAndConcurrentReservation(t *testing.T) {
	databaseURL := os.Getenv("LOGIFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOGIFLOW_TEST_DATABASE_URL is not set")
	}
	t.Chdir("../..")
	ctx := context.Background()
	if err := database.RunMigrations(ctx, databaseURL); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE order_status_history, assignments, routes, notifications, orders, driver_shifts, driver_documents, vehicle_documents, drivers, managers, warehouses, vehicles, users CASCADE`); err != nil {
		t.Fatalf("truncate test data: %v", err)
	}

	managerID := uuid.New()
	driverUserID := uuid.New()
	clientID := uuid.New()
	for _, user := range []models.User{
		{ID: managerID, Email: "manager-smoke@example.com", Slug: "manager-smoke", PasswordHash: "test", FullName: "Manager", Role: "manager", CreatedAt: time.Now()},
		{ID: driverUserID, Email: "driver-smoke@example.com", Slug: "driver-smoke", PasswordHash: "test", FullName: "Driver", Role: "driver", CreatedAt: time.Now()},
		{ID: clientID, Email: "client-smoke@example.com", Slug: "client-smoke", PasswordHash: "test", FullName: "Client", Role: "client", CreatedAt: time.Now()},
	} {
		if err := storage.Create(ctx, "users", user, pool); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}

	vehicle := models.Vehicle{ID: uuid.New(), PlateNumber: "SMOKE-001", Brand: "Test", Model: "Truck", Year: 2026, CapacityKg: 5000, CapacityM3: 30, Status: "available", Slug: "smoke-001"}
	if err := storage.Create(ctx, "vehicles", vehicle, pool); err != nil {
		t.Fatalf("create vehicle: %v", err)
	}
	driver := models.Driver{ID: uuid.New(), UserID: driverUserID, LicenseNumber: "SMOKE-LICENSE", LicenseExpiry: time.Now().AddDate(1, 0, 0), Rating: 5, Slug: "driver-smoke", Status: "available"}
	if err := storage.Create(ctx, "drivers", driver, pool); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	document := models.DriverDocument{ID: uuid.New(), DriverID: driver.ID, Type: "license", Number: driver.LicenseNumber, ValidUntil: driver.LicenseExpiry, Status: "valid", CreatedAt: time.Now()}
	if err := storage.Create(ctx, "driver_documents", document, pool); err != nil {
		t.Fatalf("create driver document: %v", err)
	}

	service := NewOrderService(pool, config.Config{})
	plannedFrom := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	plannedTo := plannedFrom.Add(2 * time.Hour)
	draft := createDraftOrder(t, ctx, pool, clientID, plannedFrom, plannedTo)
	order, err := service.SubmitOrder(ctx, draft.ID, clientID, "client")
	if err != nil {
		t.Fatalf("submit order: %v", err)
	}
	assignment, err := service.CreateAssignment(ctx, order.ID, managerID, "manager", api.AssignmentCreate{
		DriverId:    driver.ID,
		VehicleId:   vehicle.ID,
		PlannedFrom: plannedFrom,
		PlannedTo:   plannedTo,
	})
	if err != nil {
		t.Fatalf("create assignment: %v", err)
	}
	if _, err := service.AcceptAssignment(ctx, assignment.ID, driverUserID, "driver"); err != nil {
		t.Fatalf("accept assignment: %v", err)
	}
	if _, err := service.StartAssignment(ctx, assignment.ID, driverUserID, "driver"); err != nil {
		t.Fatalf("start assignment: %v", err)
	}
	if _, err := service.ArriveAssignment(ctx, assignment.ID, driverUserID, "driver"); err != nil {
		t.Fatalf("arrive assignment: %v", err)
	}
	recipient := "Smoke Recipient"
	comment := "received intact"
	if _, err := service.CompleteAssignment(ctx, assignment.ID, driverUserID, "driver", api.DeliveryComplete{RecipientName: &recipient, Comment: &comment}); err != nil {
		t.Fatalf("complete assignment: %v", err)
	}

	completedOrder, err := storage.GetOne[models.Order](ctx, pool, "orders", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", order.ID)) })
	if err != nil || completedOrder.Status != models.OrderCompleted {
		t.Fatalf("completed order: status=%v err=%v", completedOrder, err)
	}
	completedAssignment, err := storage.GetOne[models.Assignment](ctx, pool, "assignments", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", assignment.ID)) })
	if err != nil || completedAssignment.Status != models.AssignmentCompleted || completedAssignment.RecipientName == nil || *completedAssignment.RecipientName != recipient {
		t.Fatalf("completed assignment: assignment=%+v err=%v", completedAssignment, err)
	}
	unchangedDriver, _ := storage.GetOne[models.Driver](ctx, pool, "drivers", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", driver.ID)) })
	unchangedVehicle, _ := storage.GetOne[models.Vehicle](ctx, pool, "vehicles", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", vehicle.ID)) })
	if unchangedDriver.Status != "available" || unchangedVehicle.Status != "available" {
		t.Fatalf("assignment lifecycle changed resource availability: driver=%s vehicle=%s", unchangedDriver.Status, unchangedVehicle.Status)
	}

	firstOrder := createReadyOrder(t, ctx, pool, clientID, plannedFrom.Add(24*time.Hour), plannedTo.Add(24*time.Hour))
	secondOrder := createReadyOrder(t, ctx, pool, clientID, plannedFrom.Add(24*time.Hour), plannedTo.Add(24*time.Hour))
	request := api.AssignmentCreate{DriverId: driver.ID, VehicleId: vehicle.ID, PlannedFrom: plannedFrom.Add(24 * time.Hour), PlannedTo: plannedTo.Add(24 * time.Hour)}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, orderID := range []uuid.UUID{firstOrder.ID, secondOrder.ID} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.CreateAssignment(ctx, orderID, managerID, "manager", request)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var success, conflicts int
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrResourceAlreadyReserved):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent assignment error: %v", err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("concurrent assignment results: success=%d conflicts=%d", success, conflicts)
	}
}

func createReadyOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, clientID uuid.UUID, from, to time.Time) models.Order {
	t.Helper()
	order := models.Order{
		ID:                 uuid.New(),
		CreatedByID:        &clientID,
		OriginAddress:      "Origin",
		DestinationAddress: "Destination",
		CargoDescription:   "Smoke cargo",
		WeightKg:           100,
		VolumeM3:           2,
		Status:             models.OrderReadyForDispatch,
		TotalPrice:         1000,
		PickupFrom:         &from,
		PickupTo:           &to,
		CreatedAt:          time.Now(),
	}
	if err := storage.Create(ctx, "orders", order, pool); err != nil {
		t.Fatalf("create ready order: %v", err)
	}
	return order
}

func createDraftOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, clientID uuid.UUID, from, to time.Time) models.Order {
	t.Helper()
	order := createReadyOrder(t, ctx, pool, clientID, from, to)
	order.Status = models.OrderDraft
	if err := storage.Update(ctx, "orders", order, pool, func(ub *sqlbuilder.UpdateBuilder) {
		ub.Where(ub.EQ("id", order.ID))
	}); err != nil {
		t.Fatalf("make order draft: %v", err)
	}
	route := models.Route{
		ID:          uuid.New(),
		OrderID:     order.ID,
		Coordinates: json.RawMessage(`[[30.0,60.0],[30.1,60.1]]`),
		DistanceKm:  10,
		DurationSec: 900,
		Status:      "pending",
	}
	if err := storage.Create(ctx, "routes", route, pool); err != nil {
		t.Fatalf("create draft route: %v", err)
	}
	return order
}
