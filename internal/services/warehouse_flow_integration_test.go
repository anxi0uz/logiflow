package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func warehouseTestManager(t *testing.T, pool *pgxpool.Pool, warehouseID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := context.Background()
	if err := storage.Create(ctx, "users", models.User{ID: id, Email: id.String() + "@warehouse.test", Slug: id.String(), FullName: "Warehouse manager", PasswordHash: "test", Role: "manager", CreatedAt: time.Now()}, pool); err != nil {
		t.Fatal(err)
	}
	if err := storage.Create(ctx, "managers", models.Manager{ID: uuid.New(), UserID: id, WarehouseID: &warehouseID, Slug: uuid.NewString()}, pool); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestWarehouseFlowRequiresPhysicalConfirmationsAndCorrectManagers(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 340)
	destination := newExpiryFixture(t, pool, 342)
	if _, err := pool.Exec(ctx, `UPDATE orders SET destination_warehouse_id=$1 WHERE id=$2`, destination.warehouseID, f.order.ID); err != nil {
		t.Fatal(err)
	}
	originManager := warehouseTestManager(t, pool, f.warehouseID)
	destinationManager := warehouseTestManager(t, pool, destination.warehouseID)
	if _, err := f.service.GetOrder(ctx, f.order.ID, destinationManager, "manager"); err != nil {
		t.Fatalf("destination cannot inspect incoming order: %v", err)
	}
	orders, err := f.service.ListOrders(ctx, destinationManager, "manager", api.ListOrdersParams{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range orders {
		if o.ID == f.order.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("destination order missing from list")
	}
	req := api.WarehouseOperationCreate{OperationType: "receive", PackageCount: 5, CargoCondition: "Пломба цела"}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, f.driverUserID, "driver", req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("driver received cargo: %v", err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, destinationManager, "manager", req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("destination received origin cargo: %v", err)
	}
	if _, err = f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); !errors.Is(err, ErrWarehouseOperationRequired) {
		t.Fatalf("departure without loading: %v", err)
	}
	load := api.WarehouseOperationCreate{OperationType: "load", AssignmentId: &f.offer.ID, PackageCount: 5, CargoCondition: "Пломба цела"}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, originManager, "manager", load); !errors.Is(err, ErrWarehouseOperationRequired) {
		t.Fatalf("loaded without receipt: %v", err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, originManager, "manager", req); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, originManager, "manager", req); !errors.Is(err, ErrWarehouseOperationConflict) {
		t.Fatalf("duplicate receipt: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE warehouses SET status='inactive' WHERE id=$1`, f.warehouseID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, originManager, "manager", load); !errors.Is(err, ErrWarehouseInactive) {
		t.Fatalf("inactive warehouse loaded cargo: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE warehouses SET status='active' WHERE id=$1`, f.warehouseID); err != nil {
		t.Fatal(err)
	}
	wrong := load
	wrong.AssignmentId = &destination.offer.ID
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, originManager, "manager", wrong); !errors.Is(err, ErrWarehouseOperationConflict) {
		t.Fatalf("other order loading: %v", err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, originManager, "manager", load); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CancelOrder(ctx, f.order.ID, originManager, "manager", api.OrderCancel{}); !errors.Is(err, ErrWarehouseOperationConflict) {
		t.Fatalf("loaded order cancelled: %v", err)
	}
	if _, err = f.service.CreateAssignment(ctx, f.order.ID, originManager, "manager", api.AssignmentCreate{DriverId: destination.driver.ID, VehicleId: destination.vehicle.ID, PlannedFrom: f.from, PlannedTo: f.to, ReplacesAssignmentId: &f.offer.ID}); !errors.Is(err, ErrWarehouseOperationConflict) {
		t.Fatalf("loaded vehicle reassigned: %v", err)
	}
	queue, err := f.service.ListWarehouseQueue(ctx, f.warehouseID, originManager, "manager", api.ListWarehouseQueueParams{})
	if err != nil || len(queue) != 1 || queue[0].WarehouseStage != "ready_to_depart" {
		t.Fatalf("loading queue: %+v %v", queue, err)
	}
	if _, err = f.service.ListWarehouseQueue(ctx, f.warehouseID, destinationManager, "manager", api.ListWarehouseQueueParams{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign queue: %v", err)
	}
	if _, err = f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	unload := api.WarehouseOperationCreate{OperationType: "unload", AssignmentId: &f.offer.ID, PackageCount: 5, CargoCondition: "Принято без замечаний"}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, destinationManager, "manager", unload); !errors.Is(err, ErrInvalidOrderTransition) {
		t.Fatalf("unloaded before arrival: %v", err)
	}
	if _, err = f.service.ArriveAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.DeliveryComplete{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("driver self-confirmed warehouse delivery: %v", err)
	}
	if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, originManager, "manager", api.DeliveryComplete{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("origin closed incoming delivery: %v", err)
	}
	if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, destinationManager, "manager", api.DeliveryComplete{}); !errors.Is(err, ErrWarehouseOperationRequired) {
		t.Fatalf("delivery without receipt: %v", err)
	}
	// Deactivation must not strand shipments already on their way to the warehouse.
	if _, err = pool.Exec(ctx, `UPDATE warehouses SET status='inactive' WHERE id=$1`, destination.warehouseID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, destinationManager, "manager", unload); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, destinationManager, "manager", api.DeliveryComplete{}); err != nil {
		t.Fatal(err)
	}
	ops, err := f.service.ListOrderWarehouseOperations(ctx, f.order.ID, f.ownerID, "client")
	if err != nil || len(ops) != 3 {
		t.Fatalf("operation journal: %+v %v", ops, err)
	}
	if _, err = f.service.ListOrderWarehouseOperations(ctx, f.order.ID, uuid.New(), "client"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign journal read: %v", err)
	}
	queue, err = f.service.ListWarehouseQueue(ctx, f.warehouseID, originManager, "manager", api.ListWarehouseQueueParams{})
	if err != nil || len(queue) != 0 {
		t.Fatalf("closed order in active queue: %+v %v", queue, err)
	}
	include := true
	queue, err = f.service.ListWarehouseQueue(ctx, f.warehouseID, originManager, "manager", api.ListWarehouseQueueParams{IncludeClosed: &include})
	if err != nil || len(queue) != 1 || queue[0].WarehouseStage != "completed" {
		t.Fatalf("archive queue: %+v %v", queue, err)
	}
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT payload FROM integration_outbox WHERE subject='document.requested.v1' AND payload->>'reference_id'=$1 AND payload->>'type'='service_report'`, f.order.ID.String()).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var document events.DocumentRequested
	if err = json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, field := range document.Fields {
		if len(field.Label) > 0 && (field.Label == "Складская операция / receive" || field.Label == "Складская операция / load" || field.Label == "Складская операция / unload") {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("report missed warehouse confirmations: %+v", document.Fields)
	}
}

func TestWarehouseReceiptConcurrentAndDirectDeliveryUnchanged(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 350)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, f.ownerID, "admin", api.WarehouseOperationCreate{OperationType: "receive", PackageCount: 1, CargoCondition: "Без замечаний"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successful, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successful++
		} else if errors.Is(err, ErrWarehouseOperationConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successful != 1 || conflicts != 1 {
		t.Fatalf("receipt race: success=%d conflicts=%d", successful, conflicts)
	}
	direct := newExpiryFixture(t, pool, 352)
	if _, err := pool.Exec(ctx, `UPDATE orders SET origin_warehouse_id=NULL WHERE id=$1`, direct.order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := direct.service.AcceptAssignment(ctx, direct.offer.ID, direct.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := direct.service.StartAssignment(ctx, direct.offer.ID, direct.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := direct.service.ArriveAssignment(ctx, direct.offer.ID, direct.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := direct.service.CompleteAssignment(ctx, direct.offer.ID, direct.driverUserID, "driver", api.DeliveryComplete{}); err != nil {
		t.Fatal(err)
	}
}
