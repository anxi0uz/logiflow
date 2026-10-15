package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Always lock every driver before every vehicle, including when a resource is
// retained. This is the same ordering used by assignment/reassignment commands.
func (s *OrderService) lockHandoverFleet(ctx context.Context, tx pgx.Tx, old *models.Assignment, driverID, vehicleID uuid.UUID) (map[uuid.UUID]*models.Driver, map[uuid.UUID]*models.Vehicle, error) {
	driverIDs := []uuid.UUID{old.DriverID}
	if old.DriverID != driverID {
		driverIDs = append(driverIDs, driverID)
	}
	vehicleIDs := []uuid.UUID{old.VehicleID}
	if old.VehicleID != vehicleID {
		vehicleIDs = append(vehicleIDs, vehicleID)
	}
	sort.Slice(driverIDs, func(i, j int) bool { return driverIDs[i].String() < driverIDs[j].String() })
	sort.Slice(vehicleIDs, func(i, j int) bool { return vehicleIDs[i].String() < vehicleIDs[j].String() })
	drivers := map[uuid.UUID]*models.Driver{}
	vehicles := map[uuid.UUID]*models.Vehicle{}
	for _, id := range driverIDs {
		item, err := storage.GetOne[models.Driver](ctx, tx, "drivers", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", id)).ForUpdate() })
		if err != nil {
			return nil, nil, err
		}
		drivers[id] = item
	}
	for _, id := range vehicleIDs {
		item, err := storage.GetOne[models.Vehicle](ctx, tx, "vehicles", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", id)).ForUpdate() })
		if err != nil {
			return nil, nil, err
		}
		vehicles[id] = item
	}
	return drivers, vehicles, nil
}

func (s *OrderService) authorizeHandoverManager(ctx context.Context, tx pgx.Tx, order *models.Order, userID uuid.UUID, role string) error {
	if role == "admin" {
		return nil
	}
	if role != "manager" {
		return ErrForbidden
	}
	warehouseID, err := s.managerWarehouseID(ctx, tx, userID)
	if err != nil {
		return err
	}
	if order.OriginWarehouseID == nil || *order.OriginWarehouseID != warehouseID {
		return ErrForbidden
	}
	return nil
}

func (s *OrderService) CreateTripHandover(ctx context.Context, orderID, userID uuid.UUID, role string, req api.TripHandoverCreate) (*models.TripHandover, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	req.Reason, req.Location, req.CargoCondition = strings.TrimSpace(req.Reason), strings.TrimSpace(req.Location), strings.TrimSpace(req.CargoCondition)
	if req.DriverId == uuid.Nil || req.VehicleId == uuid.Nil || req.Reason == "" || req.Location == "" || req.CargoCondition == "" || utf8.RuneCountInString(req.Reason) > 1000 || utf8.RuneCountInString(req.Location) > 1000 || utf8.RuneCountInString(req.CargoCondition) > 2000 {
		return nil, ErrInvalidOrderInput
	}
	now := time.Now()
	if !req.PlannedTo.After(now) {
		return nil, ErrInvalidTimeWindow
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", orderID)).ForUpdate() })
	if err != nil {
		return nil, err
	}
	if err = s.authorizeHandoverManager(ctx, tx, order, userID, role); err != nil {
		return nil, err
	}
	if order.Status != models.OrderInTransit {
		return nil, ErrInvalidOrderTransition
	}
	old, err := storage.GetOne[models.Assignment](ctx, tx, "assignments", func(b *sqlbuilder.SelectBuilder) {
		b.Where(b.EQ("order_id", orderID), b.EQ("status", models.AssignmentActive)).ForUpdate()
	})
	if err != nil {
		return nil, err
	}
	if old.DriverID == req.DriverId && old.VehicleID == req.VehicleId {
		return nil, ErrInvalidOrderInput
	}
	drivers, vehicles, err := s.lockHandoverFleet(ctx, tx, old, req.DriverId, req.VehicleId)
	if err != nil {
		return nil, err
	}
	if err = s.validateAssignmentResources(ctx, tx, order, drivers[req.DriverId], vehicles[req.VehicleId], now, req.PlannedTo); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE trip_handovers SET status='expired' WHERE order_id=$1 AND status='pending' AND expires_at <= $2`, orderID, now); err != nil {
		return nil, err
	}
	item := models.TripHandover{ID: uuid.New(), OrderID: orderID, PreviousAssignmentID: old.ID, DriverID: req.DriverId, VehicleID: req.VehicleId, CreatedByUserID: userID, Reason: req.Reason, Location: req.Location, CargoCondition: req.CargoCondition, PlannedTo: req.PlannedTo, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute)}
	if err = storage.Create(ctx, "trip_handovers", item, tx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrAssignmentStale
		}
		return nil, err
	}
	for _, id := range []uuid.UUID{drivers[old.DriverID].UserID, drivers[req.DriverId].UserID} {
		if err = s.notify(ctx, tx, id, "Запрошена передача груза", fmt.Sprintf("Передача %s; заказ %s: %s; место: %s", item.ID, orderID, item.Reason, item.Location)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &item, nil
}

// ListIncomingTripHandovers exposes invitations before the driver has an assignment.
// Keep its scope aligned with the temporary invitation access in GetOrder.
func (s *OrderService) ListIncomingTripHandovers(ctx context.Context, userID uuid.UUID, role string) ([]models.TripHandover, error) {
	if role != "driver" {
		return nil, ErrForbidden
	}
	return storage.GetAll[models.TripHandover](ctx, "trip_handovers", s.db, func(b *sqlbuilder.SelectBuilder) {
		b.Where(
			"driver_id IN (SELECT id FROM drivers WHERE user_id = "+b.Var(userID)+")",
			b.EQ("status", "pending"), b.GreaterThan("expires_at", time.Now()),
			"order_id IN (SELECT id FROM orders WHERE status = 'in_transit')",
			"previous_assignment_id IN (SELECT id FROM assignments WHERE status = 'active')",
		).OrderBy("created_at", "id")
	})
}

func (s *OrderService) ListTripHandovers(ctx context.Context, orderID, userID uuid.UUID, role string) ([]models.TripHandover, error) {
	if _, err := s.GetOrder(ctx, orderID, userID, role); err != nil {
		return nil, err
	}
	return storage.GetAll[models.TripHandover](ctx, "trip_handovers", s.db, func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("order_id", orderID)).OrderBy("created_at", "id") })
}

func (s *OrderService) UpdateTripHandover(ctx context.Context, id, userID uuid.UUID, role, action string) (*models.TripHandover, error) {
	if role != "driver" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	seed, err := storage.GetOne[models.TripHandover](ctx, tx, "trip_handovers", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", id)) })
	if err != nil {
		return nil, err
	}
	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", seed.OrderID)).ForUpdate() })
	if err != nil {
		return nil, err
	}
	old, err := storage.GetOne[models.Assignment](ctx, tx, "assignments", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", seed.PreviousAssignmentID)).ForUpdate() })
	if err != nil {
		return nil, err
	}
	item, err := storage.GetOne[models.TripHandover](ctx, tx, "trip_handovers", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", id)).ForUpdate() })
	if err != nil {
		return nil, err
	}
	drivers, vehicles, err := s.lockHandoverFleet(ctx, tx, old, item.DriverID, item.VehicleID)
	if err != nil {
		return nil, err
	}
	outgoing, incoming := drivers[old.DriverID], drivers[item.DriverID]
	if action == "cancel" {
		if err = s.authorizeHandoverManager(ctx, tx, order, userID, role); err != nil {
			return nil, err
		}
	} else if role != "driver" || (action == "confirm-release" && outgoing.UserID != userID) || (action == "accept" && incoming.UserID != userID) {
		return nil, ErrForbidden
	}
	now := time.Now()
	if item.Status != "pending" {
		return nil, ErrAssignmentStale
	}
	if action == "cancel" {
		item.Status = "cancelled"
	} else {
		if !now.Before(item.ExpiresAt) {
			item.Status = "expired"
			if err = storage.Update(ctx, "trip_handovers", *item, tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", id)) }); err != nil {
				return nil, err
			}
			if err = tx.Commit(ctx); err != nil {
				return nil, err
			}
			return nil, ErrAssignmentExpired
		}
		if order.Status != models.OrderInTransit || old.Status != models.AssignmentActive {
			return nil, ErrInvalidOrderTransition
		}
		switch action {
		case "confirm-release":
			if item.OutgoingConfirmedAt != nil {
				return nil, ErrAssignmentStale
			}
			item.OutgoingConfirmedAt = &now
		case "accept":
			if item.OutgoingConfirmedAt == nil {
				return nil, ErrAssignmentStale
			}
			if !item.PlannedTo.After(now) {
				return nil, ErrInvalidTimeWindow
			}
			if err = s.validateAssignmentResources(ctx, tx, order, incoming, vehicles[item.VehicleID], now, item.PlannedTo); err != nil {
				return nil, err
			}
			old.Status = models.AssignmentReleased
			old.ReleasedAt = &now
			reason := "in_transit_handover"
			old.ReleaseReasonCode = &reason
			if err = storage.Update(ctx, "assignments", *old, tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", old.ID)) }); err != nil {
				return nil, err
			}
			next := models.Assignment{ID: uuid.New(), OrderID: order.ID, DriverID: item.DriverID, VehicleID: item.VehicleID, Status: models.AssignmentActive, PlannedFrom: now, PlannedTo: item.PlannedTo, CreatedByUserID: item.CreatedByUserID, Source: "manual", AssignedAt: now, OfferExpiresAt: now.Add(15 * time.Minute), AcceptedAt: &now, StartedAt: &now}
			if err = storage.Create(ctx, "assignments", next, tx); err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && (pgErr.Code == "23P01" || pgErr.Code == "23505") {
					return nil, ErrResourceAlreadyReserved
				}
				return nil, err
			}
			order.DriverID = &next.DriverID
			if err = storage.Update(ctx, "orders", *order, tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", order.ID)) }); err != nil {
				return nil, err
			}
			item.Status = "completed"
			item.CompletedAt = &now
			item.NextAssignmentID = &next.ID
			if err = s.enqueueDispatchAfterRelease(ctx, tx, old); err != nil {
				return nil, err
			}
			outgoingUser, err := storage.GetOne[models.User](ctx, tx, "users", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", outgoing.UserID)) })
			if err != nil {
				return nil, err
			}
			if err = s.enqueueOperationalDocument(ctx, tx, order, &next, "cargo_handover", item.ID, userID, []documentField{
				{"Причина замены", item.Reason}, {"Место передачи", item.Location}, {"Состояние груза", item.CargoCondition},
				{"Предыдущее назначение", old.ID.String()}, {"Предыдущий водитель", outgoingUser.FullName + " / " + old.DriverID.String()}, {"Предыдущая машина", vehicles[old.VehicleID].PlateNumber + " / " + old.VehicleID.String()},
				{"Передающий водитель подтвердил", item.OutgoingConfirmedAt.UTC().Format(time.RFC3339)},
				{"Принимающий водитель подтвердил", now.UTC().Format(time.RFC3339)},
			}, outgoing.UserID); err != nil {
				return nil, err
			}
			if err = s.enqueueOperationalDocument(ctx, tx, order, &next, "trip_assignment", next.ID, userID, nil); err != nil {
				return nil, err
			}
			if order.CreatedByID != nil {
				if err = s.notify(ctx, tx, *order.CreatedByID, "Замена ресурсов в пути", fmt.Sprintf("Заказ %s: %s; место: %s. История сохранена.", order.ID, item.Reason, item.Location)); err != nil {
					return nil, err
				}
			}
		default:
			return nil, ErrInvalidOrderInput
		}
	}
	if err = storage.Update(ctx, "trip_handovers", *item, tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", id)) }); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}
