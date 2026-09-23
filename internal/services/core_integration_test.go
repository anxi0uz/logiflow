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
	warehouse := models.Warehouse{ID: uuid.New(), Name: "Smoke warehouse", Slug: "smoke-warehouse", Address: "Origin", City: "Test", Latitude: 60, Longitude: 30, CreatedAt: time.Now()}
	if err := storage.Create(ctx, "warehouses", warehouse, pool); err != nil {
		t.Fatalf("create warehouse: %v", err)
	}
	if err := storage.Create(ctx, "managers", models.Manager{ID: uuid.New(), UserID: managerID, WarehouseID: &warehouse.ID, Slug: "manager-smoke"}, pool); err != nil {
		t.Fatalf("create manager: %v", err)
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
	vehicleDocument := models.VehicleDocument{ID: uuid.New(), VehicleID: vehicle.ID, Type: "registration", Number: "SMOKE-REG", ValidUntil: time.Now().AddDate(1, 0, 0), Status: "valid", CreatedAt: time.Now()}
	if err := storage.Create(ctx, "vehicle_documents", vehicleDocument, pool); err != nil {
		t.Fatalf("create vehicle document: %v", err)
	}

	service := NewOrderService(pool, config.Config{})
	plannedFrom := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	plannedTo := plannedFrom.Add(2 * time.Hour)
	draft := createDraftOrder(t, ctx, pool, clientID, warehouse.ID, plannedFrom, plannedTo)
	otherWarehouse := models.Warehouse{ID: uuid.New(), Name: "Other warehouse", Slug: "other-warehouse", Address: "Other", City: "Test", Latitude: 60, Longitude: 31, CreatedAt: time.Now()}
	if err := storage.Create(ctx, "warehouses", otherWarehouse, pool); err != nil {
		t.Fatalf("create other warehouse: %v", err)
	}
	otherDraft := createDraftOrder(t, ctx, pool, clientID, otherWarehouse.ID, plannedFrom, plannedTo)
	newWeight := float32(200)
	addressOnly := createDraftOrder(t, ctx, pool, clientID, warehouse.ID, plannedFrom, plannedTo)
	addressOnly.OriginWarehouseID = nil
	if err := storage.Update(ctx, "orders", addressOnly, pool, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", addressOnly.ID)) }); err != nil {
		t.Fatalf("remove origin warehouse: %v", err)
	}
	visibleOrders, err := service.ListOrders(ctx, managerID, "manager", api.ListOrdersParams{})
	if err != nil || len(visibleOrders) != 1 || visibleOrders[0].ID != draft.ID {
		t.Fatalf("manager order scope: %+v err=%v", visibleOrders, err)
	}
	if _, err := service.ListOrders(ctx, uuid.New(), "manager", api.ListOrdersParams{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager without warehouse saw orders: %v", err)
	}
	if _, err := service.GetOrder(ctx, otherDraft.ID, managerID, "manager"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager read other warehouse order: %v", err)
	}
	if _, err := service.GetOrder(ctx, addressOnly.ID, managerID, "manager"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager read address-only order: %v", err)
	}
	if _, err := service.UpdateDraftOrder(ctx, otherDraft.ID, managerID, "manager", api.OrderDraftUpdate{WeightKg: &newWeight}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager edited other warehouse draft: %v", err)
	}
	if _, err := service.SubmitOrder(ctx, otherDraft.ID, managerID, "manager"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager submitted other warehouse order: %v", err)
	}
	if _, err := service.CancelOrder(ctx, otherDraft.ID, managerID, "manager", api.OrderCancel{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager cancelled other warehouse order: %v", err)
	}
	if _, err := service.CreateAssignment(ctx, otherDraft.ID, managerID, "manager", api.AssignmentCreate{DriverId: driver.ID, VehicleId: vehicle.ID, PlannedFrom: plannedFrom, PlannedTo: plannedTo}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager assigned other warehouse order: %v", err)
	}
	ordersReport, err := service.GetOrdersReport(ctx, managerID, "manager", api.GetOrdersReportParams{})
	if err != nil || len(ordersReport) != 1 || ordersReport[0].ID != draft.ID {
		t.Fatalf("manager report scope: %+v err=%v", ordersReport, err)
	}
	dashboard, err := service.GetDashboard(ctx, managerID, "manager")
	if err != nil || dashboard.Orders.Total != 1 {
		t.Fatalf("manager dashboard scope: %+v err=%v", dashboard, err)
	}
	adminDashboard, err := service.GetDashboard(ctx, clientID, "admin")
	if err != nil || adminDashboard.Orders.Total != 3 {
		t.Fatalf("admin dashboard scope: %+v err=%v", adminDashboard, err)
	}
	updated, err := service.UpdateDraftOrder(ctx, draft.ID, clientID, "client", api.OrderDraftUpdate{WeightKg: &newWeight})
	if err != nil || updated.WeightKg != 200 {
		t.Fatalf("update draft: order=%+v err=%v", updated, err)
	}
	order, err := service.SubmitOrder(ctx, draft.ID, clientID, "client")
	if err != nil {
		t.Fatalf("submit order: %v", err)
	}
	if _, err := service.UpdateDraftOrder(ctx, draft.ID, clientID, "client", api.OrderDraftUpdate{WeightKg: &newWeight}); !errors.Is(err, ErrInvalidOrderTransition) {
		t.Fatalf("submitted draft editable: %v", err)
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
	assignedAt := time.Now()
	rejectedAt := assignedAt.Add(time.Minute)
	otherAssignment := models.Assignment{
		ID:              uuid.New(),
		OrderID:         otherDraft.ID,
		DriverID:        driver.ID,
		VehicleID:       vehicle.ID,
		Status:          models.AssignmentRejected,
		PlannedFrom:     plannedFrom,
		PlannedTo:       plannedTo,
		CreatedByUserID: managerID,
		Source:          "manual",
		AssignedAt:      assignedAt,
		OfferExpiresAt:  assignedAt.Add(15 * time.Minute),
		RejectedAt:      &rejectedAt,
	}
	if err := storage.Create(ctx, "assignments", otherAssignment, pool); err != nil {
		t.Fatalf("create other warehouse assignment: %v", err)
	}
	managerAssignments, err := service.ListAssignments(ctx, managerID, "manager", api.ListAssignmentsParams{})
	if err != nil || len(managerAssignments) != 1 || managerAssignments[0].ID != assignment.ID {
		t.Fatalf("manager assignment scope: %+v err=%v", managerAssignments, err)
	}
	if _, err := service.ListOrderAssignments(ctx, otherDraft.ID, managerID, "manager"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("manager assignment history for other warehouse: %v", err)
	}
	visible, err := service.ListAssignments(ctx, driverUserID, "driver", api.ListAssignmentsParams{})
	if err != nil || len(visible) != 2 || (visible[0].ID != assignment.ID && visible[1].ID != assignment.ID) {
		t.Fatalf("driver offers: %+v err=%v", visible, err)
	}
	if _, err := service.ListAssignments(ctx, clientID, "client", api.ListAssignmentsParams{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("client assignments visible: %v", err)
	}
	history, err := service.ListOrderAssignments(ctx, order.ID, managerID, "manager")
	if err != nil || len(history) != 1 {
		t.Fatalf("manager assignment history: %+v err=%v", history, err)
	}
	notices, err := storage.GetAll[models.Notification](ctx, "notifications", pool, func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("user_id", driverUserID)) })
	if err != nil || len(notices) != 1 {
		t.Fatalf("driver offer notification: %+v err=%v", notices, err)
	}
	if _, err := service.AcceptAssignment(ctx, assignment.ID, clientID, "driver"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong driver accepted: %v", err)
	}
	managerNotices, err := storage.GetAll[models.Notification](ctx, "notifications", pool, func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("user_id", managerID)) })
	if err != nil || len(managerNotices) != 0 {
		t.Fatalf("failed accept produced notification: %+v err=%v", managerNotices, err)
	}
	if _, err := service.AcceptAssignment(ctx, assignment.ID, driverUserID, "driver"); err != nil {
		t.Fatalf("accept assignment: %v", err)
	}
	if _, err := service.AcceptAssignment(ctx, assignment.ID, driverUserID, "driver"); !errors.Is(err, ErrAssignmentStale) {
		t.Fatalf("double accept: %v", err)
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
	if _, err := service.CompleteAssignment(ctx, assignment.ID, driverUserID, "driver", api.DeliveryComplete{}); !errors.Is(err, ErrInvalidOrderTransition) {
		t.Fatalf("double complete: %v", err)
	}
	clientNotices, err := storage.GetAll[models.Notification](ctx, "notifications", pool, func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("user_id", clientID)) })
	if err != nil || len(clientNotices) != 4 {
		t.Fatalf("delivery notifications: %+v err=%v", clientNotices, err)
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

	firstOrder := createReadyOrder(t, ctx, pool, clientID, warehouse.ID, plannedFrom.Add(24*time.Hour), plannedTo.Add(24*time.Hour))
	secondOrder := createReadyOrder(t, ctx, pool, clientID, warehouse.ID, plannedFrom.Add(24*time.Hour), plannedTo.Add(24*time.Hour))
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

	missingDocumentVehicle := models.Vehicle{ID: uuid.New(), PlateNumber: "SMOKE-002", CapacityKg: 5000, CapacityM3: 30, Status: "available", Slug: "smoke-002"}
	if err := storage.Create(ctx, "vehicles", missingDocumentVehicle, pool); err != nil {
		t.Fatalf("create second vehicle: %v", err)
	}
	laterFrom, laterTo := plannedFrom.Add(48*time.Hour), plannedTo.Add(48*time.Hour)
	docOrder := createReadyOrder(t, ctx, pool, clientID, warehouse.ID, laterFrom, laterTo)
	badRequest := api.AssignmentCreate{DriverId: driver.ID, VehicleId: missingDocumentVehicle.ID, PlannedFrom: laterFrom, PlannedTo: laterTo}
	if _, err := service.CreateAssignment(ctx, docOrder.ID, managerID, "manager", badRequest); !errors.Is(err, ErrVehicleDocumentInvalid) {
		t.Fatalf("vehicle without registration accepted: %v", err)
	}
	if err := storage.Create(ctx, "vehicle_documents", models.VehicleDocument{ID: uuid.New(), VehicleID: missingDocumentVehicle.ID, Type: "registration", Number: "EXPIRED", ValidUntil: time.Now().AddDate(0, 0, -1), Status: "valid", CreatedAt: time.Now()}, pool); err != nil {
		t.Fatalf("create expired document: %v", err)
	}
	if _, err := service.CreateAssignment(ctx, docOrder.ID, managerID, "manager", badRequest); !errors.Is(err, ErrVehicleDocumentInvalid) {
		t.Fatalf("expired registration accepted: %v", err)
	}
	if err := storage.Create(ctx, "vehicle_documents", models.VehicleDocument{ID: uuid.New(), VehicleID: missingDocumentVehicle.ID, Type: "registration", Number: "VALID", ValidUntil: time.Now().AddDate(1, 0, 0), Status: "valid", CreatedAt: time.Now()}, pool); err != nil {
		t.Fatalf("create valid document: %v", err)
	}
	offer, err := service.CreateAssignment(ctx, docOrder.ID, managerID, "manager", badRequest)
	if err != nil {
		t.Fatalf("create assignment with valid registration: %v", err)
	}
	if _, err := service.RejectAssignment(ctx, offer.ID, driverUserID, "driver", api.AssignmentReject{ReasonCode: "unavailable"}); err != nil {
		t.Fatalf("reject assignment: %v", err)
	}
	secondOffer, err := service.CreateAssignment(ctx, docOrder.ID, managerID, "manager", badRequest)
	if err != nil {
		t.Fatalf("reassign after rejection: %v", err)
	}
	badRequest.ReplacesAssignmentId = &secondOffer.ID
	replacedOffer, err := service.CreateAssignment(ctx, docOrder.ID, managerID, "manager", badRequest)
	if err != nil {
		t.Fatalf("replace pending offer: %v", err)
	}
	replaced, err := storage.GetOne[models.Assignment](ctx, pool, "assignments", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", secondOffer.ID)) })
	if err != nil || replaced.Status != models.AssignmentReleased {
		t.Fatalf("replacement did not release old offer: %+v err=%v", replaced, err)
	}
	if _, err := service.CancelOrder(ctx, docOrder.ID, managerID, "manager", api.OrderCancel{}); err != nil {
		t.Fatalf("cancel assignment: %v", err)
	}
	released, err := storage.GetOne[models.Assignment](ctx, pool, "assignments", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", replacedOffer.ID)) })
	if err != nil || released.Status != models.AssignmentReleased {
		t.Fatalf("cancel did not release reservation: %+v err=%v", released, err)
	}
	history, err = service.ListOrderAssignments(ctx, docOrder.ID, managerID, "manager")
	if err != nil || len(history) != 3 {
		t.Fatalf("rejection history lost: %+v err=%v", history, err)
	}
}

func createReadyOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, clientID, warehouseID uuid.UUID, from, to time.Time) models.Order {
	t.Helper()
	order := models.Order{
		ID:                 uuid.New(),
		CreatedByID:        &clientID,
		OriginWarehouseID:  &warehouseID,
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

func createDraftOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, clientID, warehouseID uuid.UUID, from, to time.Time) models.Order {
	t.Helper()
	order := createReadyOrder(t, ctx, pool, clientID, warehouseID, from, to)
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
