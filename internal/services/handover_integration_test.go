package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
	"github.com/google/uuid"
)

func TestTripHandoverPreservesHistoryAndRevalidatesReservation(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 300)
	target := newExpiryFixture(t, pool, 302)
	if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	loadTestAssignment(t, f.service, f.offer, f.ownerID)
	if _, err := f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	original, err := f.service.GetOrder(ctx, f.order.ID, f.ownerID, "client")
	if err != nil {
		t.Fatal(err)
	}
	req := api.TripHandoverCreate{DriverId: target.driver.ID, VehicleId: f.vehicle.ID, PlannedTo: time.Now().Add(time.Hour), Reason: "Смена водителя", Location: "Площадка отдыха", CargoCondition: "Без замечаний"}
	if _, err = f.service.CreateTripHandover(ctx, f.order.ID, f.ownerID, "client", req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("client proposed handover: %v", err)
	}
	h, err := f.service.CreateTripHandover(ctx, f.order.ID, f.ownerID, "admin", req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateTripHandover(ctx, f.order.ID, f.ownerID, "admin", req); !errors.Is(err, ErrAssignmentStale) {
		t.Fatalf("duplicate pending: %v", err)
	}
	if _, err = f.service.GetOrder(ctx, f.order.ID, target.driverUserID, "driver"); err != nil {
		t.Fatalf("incoming cannot inspect invitation: %v", err)
	}
	if _, err = f.service.UpdateTripHandover(ctx, h.ID, target.driverUserID, "driver", "accept"); !errors.Is(err, ErrAssignmentStale) {
		t.Fatalf("accepted before release: %v", err)
	}
	if _, err = f.service.UpdateTripHandover(ctx, h.ID, target.driverUserID, "driver", "confirm-release"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong outgoing: %v", err)
	}
	if _, err = f.service.UpdateTripHandover(ctx, h.ID, f.driverUserID, "driver", "confirm-release"); err != nil {
		t.Fatal(err)
	}

	// A target that was free at request time gets reserved before acceptance.
	start := time.Now().Add(time.Minute)
	end := start.Add(20 * time.Minute)
	conflict := createReadyOrder(t, ctx, pool, f.ownerID, f.warehouseID, start, end)
	_, err = f.service.CreateAssignment(ctx, conflict.ID, f.ownerID, "admin", api.AssignmentCreate{DriverId: target.driver.ID, VehicleId: f.vehicle.ID, PlannedFrom: start, PlannedTo: end})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.UpdateTripHandover(ctx, h.ID, target.driverUserID, "driver", "accept"); !errors.Is(err, ErrResourceAlreadyReserved) {
		t.Fatalf("missing reservation check: %v", err)
	}
	assertOfferStatus(t, f, models.AssignmentActive)
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM integration_outbox WHERE subject='document.requested.v1' AND payload->>'reference_id'=$1`, h.ID.String()).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed handover emitted document: %d %v", count, err)
	}
	if _, err = f.service.CancelOrder(ctx, conflict.ID, f.ownerID, "admin", api.OrderCancel{}); err != nil {
		t.Fatal(err)
	}
	handed, err := f.service.UpdateTripHandover(ctx, h.ID, target.driverUserID, "driver", "accept")
	if err != nil {
		t.Fatal(err)
	}
	if handed.Status != "completed" || handed.NextAssignmentID == nil || handed.OutgoingConfirmedAt == nil {
		t.Fatalf("handover %+v", handed)
	}
	assertOfferStatus(t, f, models.AssignmentReleased)
	current, err := f.service.GetOrder(ctx, f.order.ID, f.ownerID, "client")
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != models.OrderInTransit || current.DriverID == nil || *current.DriverID != target.driver.ID || !current.StartedAt.Equal(*original.StartedAt) {
		t.Fatalf("order lifecycle rewritten: %+v", current)
	}
	if _, err = f.service.GetOrder(ctx, f.order.ID, f.driverUserID, "driver"); err != nil {
		t.Fatalf("former driver lost history: %v", err)
	}
	if _, err = f.service.ArriveAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); !errors.Is(err, ErrInvalidOrderTransition) {
		t.Fatalf("former assignment still usable: %v", err)
	}
	if _, err = f.service.UpdateTripHandover(ctx, h.ID, target.driverUserID, "driver", "accept"); !errors.Is(err, ErrAssignmentStale) {
		t.Fatalf("replayed transfer accepted: %v", err)
	}
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT payload FROM integration_outbox WHERE subject='document.requested.v1' AND payload->>'reference_id'=$1`, h.ID.String()).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var document events.DocumentRequested
	if err = json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	if document.Type != "cargo_handover" || document.ReferenceID != h.ID || len(document.Recipients) < 3 {
		t.Fatalf("document missing participants: %+v", document)
	}
	before := string(payload)
	if _, err = pool.Exec(ctx, `UPDATE users SET full_name='Changed after handover' WHERE id=$1`, target.driverUserID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT payload FROM integration_outbox WHERE id=$1`, document.EventID).Scan(&payload); err != nil || string(payload) != before {
		t.Fatal("issued snapshot changed after profile edit")
	}
}

func TestTripHandoverExpiryCancellationAndWrongActor(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 310)
	target := newExpiryFixture(t, pool, 312)
	req := api.TripHandoverCreate{DriverId: target.driver.ID, VehicleId: target.vehicle.ID, PlannedTo: time.Now().Add(time.Hour), Reason: "Поломка машины", Location: "Стоянка", CargoCondition: "Упаковка целая"}
	if _, err := f.service.CreateTripHandover(ctx, f.order.ID, f.ownerID, "admin", req); !errors.Is(err, ErrInvalidOrderTransition) {
		t.Fatalf("handover before start: %v", err)
	}
	if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	loadTestAssignment(t, f.service, f.offer, f.ownerID)
	if _, err := f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	h, err := f.service.CreateTripHandover(ctx, f.order.ID, f.ownerID, "admin", req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.UpdateTripHandover(ctx, h.ID, uuid.New(), "driver", "accept"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unrelated actor accepted: %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE trip_handovers SET created_at=NOW()-INTERVAL '16 minutes',expires_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.UpdateTripHandover(ctx, h.ID, target.driverUserID, "driver", "accept"); !errors.Is(err, ErrAssignmentExpired) {
		t.Fatalf("expired request accepted: %v", err)
	}
	assertOfferStatus(t, f, models.AssignmentActive)
	h, err = f.service.CreateTripHandover(ctx, f.order.ID, f.ownerID, "admin", req)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := f.service.UpdateTripHandover(ctx, h.ID, f.ownerID, "admin", "cancel")
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}
	assertOfferStatus(t, f, models.AssignmentActive)
}

func TestTripHandoverVehicleOnly(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 320)
	target := newExpiryFixture(t, pool, 322)
	if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	loadTestAssignment(t, f.service, f.offer, f.ownerID)
	if _, err := f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	req := api.TripHandoverCreate{DriverId: f.driver.ID, VehicleId: f.vehicle.ID, PlannedTo: time.Now().Add(time.Hour), Reason: "Замена машины", Location: "Стоянка", CargoCondition: "Без повреждений"}
	if _, err := f.service.CreateTripHandover(ctx, f.order.ID, f.ownerID, "admin", req); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("unchanged resources accepted: %v", err)
	}
	req.VehicleId = target.vehicle.ID
	h, err := f.service.CreateTripHandover(ctx, f.order.ID, f.ownerID, "admin", req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.UpdateTripHandover(ctx, h.ID, f.driverUserID, "driver", "confirm-release"); err != nil {
		t.Fatal(err)
	}
	h, err = f.service.UpdateTripHandover(ctx, h.ID, f.driverUserID, "driver", "accept")
	if err != nil {
		t.Fatal(err)
	}
	var driverID, vehicleID uuid.UUID
	if err = pool.QueryRow(ctx, `SELECT driver_id,vehicle_id FROM assignments WHERE id=$1 AND status='active'`, h.NextAssignmentID).Scan(&driverID, &vehicleID); err != nil || driverID != f.driver.ID || vehicleID != target.vehicle.ID {
		t.Fatalf("vehicle replacement: %s %s %v", driverID, vehicleID, err)
	}
	var payload []byte
	if err = pool.QueryRow(ctx, `SELECT payload FROM integration_outbox WHERE subject='document.requested.v1' AND payload->>'reference_id'=$1`, h.ID.String()).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var document events.DocumentRequested
	if err = json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	foundPlate := false
	for _, field := range document.Fields {
		if field.Label == "Предыдущая машина" && field.Value == f.vehicle.PlateNumber+" / "+f.vehicle.ID.String() {
			foundPlate = true
		}
	}
	if !foundPlate {
		t.Fatal("handover snapshot lost previous vehicle plate")
	}
	assertOfferStatus(t, f, models.AssignmentReleased)
}
