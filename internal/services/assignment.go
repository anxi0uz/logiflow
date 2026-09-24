package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *OrderService) SubmitOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin submit order: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id)).ForUpdate()
	})
	if err != nil {
		return nil, err
	}
	if role != "manager" && role != "admin" && (role != "client" || order.CreatedByID == nil || *order.CreatedByID != userID) {
		return nil, ErrForbidden
	}
	if role == "manager" {
		warehouseID, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if order.OriginWarehouseID == nil || *order.OriginWarehouseID != warehouseID {
			return nil, ErrForbidden
		}
	}
	if !models.CanTransitionOrder(order.Status, models.OrderReadyForDispatch) {
		return nil, ErrInvalidOrderTransition
	}
	if order.DestinationAddress == "" || order.WeightKg < 0 || order.VolumeM3 < 0 || order.PickupFrom == nil || order.PickupTo == nil || !order.PickupFrom.Before(*order.PickupTo) {
		return nil, ErrInvalidTimeWindow
	}
	route, err := storage.GetOne[models.Route](ctx, tx, "routes", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("order_id", id))
	})
	if err != nil {
		return nil, fmt.Errorf("order route is not ready: %w", err)
	}
	if order.TotalPrice == nil || route.DistanceKm == nil || route.DurationSec == nil || len(route.Coordinates) == 0 {
		return nil, ErrInvalidOrderInput
	}

	now := time.Now()
	from := order.Status
	order.Status = models.OrderReadyForDispatch
	order.SubmittedAt = &now
	if err := storage.Update(ctx, "orders", *order, tx, func(ub *sqlbuilder.UpdateBuilder) {
		ub.Where(ub.EQ("id", id))
	}); err != nil {
		return nil, err
	}
	if err := s.recordOrderStatus(ctx, tx, order.ID, from, order.Status, userID, nil); err != nil {
		return nil, err
	}
	if order.CreatedByID != nil && *order.CreatedByID != userID {
		if err := s.notify(ctx, tx, *order.CreatedByID, "Заказ отправлен", fmt.Sprintf("Заказ %s готов к назначению водителя", id)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit submit order: %w", err)
	}
	return order, nil
}

func (s *OrderService) CancelOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.OrderCancel) (*models.Order, error) {
	if role != "client" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cancel order: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id)).ForUpdate()
	})
	if err != nil {
		return nil, err
	}
	assignments, err := storage.GetAll[models.Assignment](ctx, "assignments", tx, func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(
			sb.EQ("order_id", id),
			sb.In("status", models.AssignmentPendingAcceptance, models.AssignmentAccepted),
		).OrderBy("id").ForUpdate()
	})
	if err != nil {
		return nil, err
	}
	if len(assignments) > 1 {
		return nil, ErrAssignmentStale
	}
	if len(assignments) == 1 {
		if _, err := storage.GetOne[models.Driver](ctx, tx, "drivers", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("id", assignments[0].DriverID)).ForUpdate()
		}); err != nil {
			return nil, ErrDriverNotEligible
		}
		if _, err := storage.GetOne[models.Vehicle](ctx, tx, "vehicles", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("id", assignments[0].VehicleID)).ForUpdate()
		}); err != nil {
			return nil, ErrVehicleNotOperational
		}
	}

	// Revalidate ownership and state after the full lock set is held.
	if role == "manager" {
		warehouseID, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if order.OriginWarehouseID == nil || *order.OriginWarehouseID != warehouseID {
			return nil, ErrForbidden
		}
	}
	if role == "client" && (order.CreatedByID == nil || *order.CreatedByID != userID || (order.Status != models.OrderDraft && order.Status != models.OrderReadyForDispatch)) {
		return nil, ErrForbidden
	}
	if !models.CanTransitionOrder(order.Status, models.OrderCancelled) {
		return nil, ErrInvalidOrderTransition
	}
	now := time.Now()
	for _, assignment := range assignments {
		assignment.Status = models.AssignmentReleased
		assignment.ReleasedAt = &now
		reason := "order_cancelled"
		assignment.ReleaseReasonCode = &reason
		if err := storage.Update(ctx, "assignments", assignment, tx, func(ub *sqlbuilder.UpdateBuilder) {
			ub.Where(ub.EQ("id", assignment.ID))
		}); err != nil {
			return nil, err
		}
		driver, err := storage.GetOne[models.Driver](ctx, tx, "drivers", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", assignment.DriverID)) })
		if err != nil {
			return nil, err
		}
		if err := s.notify(ctx, tx, driver.UserID, "Назначение отменено", fmt.Sprintf("Заказ %s отменён, назначение %s снято", id, assignment.ID)); err != nil {
			return nil, err
		}
	}
	from := order.Status
	order.Status = models.OrderCancelled
	order.CancelledAt = &now
	if err := storage.Update(ctx, "orders", *order, tx, func(ub *sqlbuilder.UpdateBuilder) {
		ub.Where(ub.EQ("id", id))
	}); err != nil {
		return nil, err
	}
	if err := s.recordOrderStatus(ctx, tx, id, from, order.Status, userID, req.ReasonCode); err != nil {
		return nil, err
	}
	if order.CreatedByID != nil && *order.CreatedByID != userID {
		if err := s.notify(ctx, tx, *order.CreatedByID, "Заказ отменён", fmt.Sprintf("Заказ %s отменён менеджером", id)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cancel order: %w", err)
	}
	return order, nil
}

func (s *OrderService) CreateAssignment(ctx context.Context, orderID uuid.UUID, userID uuid.UUID, role string, req api.AssignmentCreate) (*models.Assignment, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	if role == "manager" {
		if _, err := s.GetOrder(ctx, orderID, userID, role); err != nil {
			return nil, err
		}
	}
	if !req.PlannedFrom.Before(req.PlannedTo) || req.PlannedFrom.Before(time.Now()) {
		return nil, ErrInvalidTimeWindow
	}
	if err := s.expireStaleAssignments(ctx, orderID, req.DriverId, req.VehicleId); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin assignment: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", orderID)).ForUpdate()
	})
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var previous *models.Assignment
	if req.ReplacesAssignmentId != nil {
		seed, err := storage.GetOne[models.Assignment](ctx, tx, "assignments", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("id", *req.ReplacesAssignmentId))
		})
		if err != nil || seed.OrderID != orderID {
			return nil, ErrAssignmentStale
		}
		previous, err = storage.GetOne[models.Assignment](ctx, tx, "assignments", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("id", *req.ReplacesAssignmentId)).ForUpdate()
		})
		if err != nil || previous.OrderID != seed.OrderID || previous.DriverID != seed.DriverID || previous.VehicleID != seed.VehicleID {
			return nil, ErrAssignmentStale
		}
	}

	driverIDs := []uuid.UUID{req.DriverId}
	vehicleIDs := []uuid.UUID{req.VehicleId}
	if previous != nil {
		if previous.DriverID != req.DriverId {
			driverIDs = append(driverIDs, previous.DriverID)
		}
		if previous.VehicleID != req.VehicleId {
			vehicleIDs = append(vehicleIDs, previous.VehicleID)
		}
	}
	sort.Slice(driverIDs, func(i, j int) bool { return driverIDs[i].String() < driverIDs[j].String() })
	sort.Slice(vehicleIDs, func(i, j int) bool { return vehicleIDs[i].String() < vehicleIDs[j].String() })
	var driver, previousDriver *models.Driver
	for _, driverID := range driverIDs {
		locked, err := storage.GetOne[models.Driver](ctx, tx, "drivers", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("id", driverID)).ForUpdate()
		})
		if err != nil {
			return nil, ErrDriverNotEligible
		}
		if driverID == req.DriverId {
			driver = locked
		}
		if previous != nil && driverID == previous.DriverID {
			previousDriver = locked
		}
	}
	var vehicle *models.Vehicle
	for _, vehicleID := range vehicleIDs {
		locked, err := storage.GetOne[models.Vehicle](ctx, tx, "vehicles", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("id", vehicleID)).ForUpdate()
		})
		if err != nil {
			return nil, ErrVehicleNotOperational
		}
		if vehicleID == req.VehicleId {
			vehicle = locked
		}
	}

	// Everything below is validated only after the complete lock set is held.
	if role == "manager" {
		warehouseID, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if order.OriginWarehouseID == nil || *order.OriginWarehouseID != warehouseID {
			return nil, ErrForbidden
		}
	}
	if previous == nil {
		if order.Status != models.OrderReadyForDispatch {
			return nil, ErrInvalidOrderTransition
		}
	} else {
		if previous.OrderID != orderID || (previous.Status != models.AssignmentPendingAcceptance && previous.Status != models.AssignmentAccepted) {
			return nil, ErrAssignmentStale
		}
		if order.Status != models.OrderReadyForDispatch && order.Status != models.OrderAssigned {
			return nil, ErrInvalidOrderTransition
		}
	}
	if err := s.validateAssignmentResources(ctx, tx, order, driver, vehicle, req.PlannedFrom, req.PlannedTo); err != nil {
		return nil, err
	}
	source := "manual"
	if req.Source != nil {
		source = string(*req.Source)
	}
	if source != "manual" && source != "dispatch_recommendation" {
		return nil, ErrAssignmentStale
	}
	if previous != nil {
		previous.Status = models.AssignmentReleased
		previous.ReleasedAt = &now
		reason := "reassigned"
		previous.ReleaseReasonCode = &reason
		if err := storage.Update(ctx, "assignments", *previous, tx, func(ub *sqlbuilder.UpdateBuilder) {
			ub.Where(ub.EQ("id", previous.ID))
		}); err != nil {
			return nil, err
		}
		if err := s.notify(ctx, tx, previousDriver.UserID, "Назначение заменено", fmt.Sprintf("Предложение %s по заказу %s отозвано", previous.ID, orderID)); err != nil {
			return nil, err
		}
		if order.Status == models.OrderAssigned {
			from := order.Status
			order.Status = models.OrderReadyForDispatch
			order.DriverID = nil
			order.AssignedAt = nil
			if err := storage.Update(ctx, "orders", *order, tx, func(ub *sqlbuilder.UpdateBuilder) {
				ub.Where(ub.EQ("id", order.ID))
			}); err != nil {
				return nil, err
			}
			if err := s.recordOrderStatus(ctx, tx, order.ID, from, order.Status, userID, &reason); err != nil {
				return nil, err
			}
		}
	}

	assignment := models.Assignment{
		ID:                      uuid.New(),
		OrderID:                 orderID,
		DriverID:                req.DriverId,
		VehicleID:               req.VehicleId,
		Status:                  models.AssignmentPendingAcceptance,
		PlannedFrom:             req.PlannedFrom,
		PlannedTo:               req.PlannedTo,
		CreatedByUserID:         userID,
		Source:                  source,
		RecommendationID:        req.RecommendationId,
		RecommendationCreatedAt: req.RecommendationCreatedAt,
		AssignedAt:              now,
		OfferExpiresAt:          now.Add(15 * time.Minute),
	}
	if err := storage.Create(ctx, "assignments", assignment, tx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == "23P01" || pgErr.Code == "23505") {
			return nil, ErrResourceAlreadyReserved
		}
		return nil, fmt.Errorf("create assignment: %w", err)
	}
	if err := s.notify(ctx, tx, driver.UserID, "Новое назначение", fmt.Sprintf("Предложение %s по заказу %s: примите или отклоните до истечения срока", assignment.ID, orderID)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit assignment: %w", err)
	}
	return &assignment, nil
}

func (s *OrderService) AcceptAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error) {
	if role != "driver" {
		return nil, ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	order, assignment, driver, vehicle, err := s.lockAssignmentContext(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if driver.UserID != userID {
		return nil, ErrForbidden
	}
	if assignment.Status != models.AssignmentPendingAcceptance {
		return nil, ErrAssignmentStale
	}
	now := time.Now()
	if !now.Before(assignment.OfferExpiresAt) {
		assignment.Status = models.AssignmentExpired
		if err := storage.Update(ctx, "assignments", *assignment, tx, func(ub *sqlbuilder.UpdateBuilder) {
			ub.Where(ub.EQ("id", id))
		}); err != nil {
			return nil, err
		}
		if err := s.notify(ctx, tx, assignment.CreatedByUserID, "Предложение истекло", fmt.Sprintf("Назначение %s по заказу %s не принято вовремя", id, order.ID)); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, ErrAssignmentExpired
	}
	if order.Status != models.OrderReadyForDispatch {
		return nil, ErrInvalidOrderTransition
	}
	if err := s.validateAssignmentResources(ctx, tx, order, driver, vehicle, assignment.PlannedFrom, assignment.PlannedTo); err != nil {
		return nil, err
	}
	assignment.Status = models.AssignmentAccepted
	assignment.AcceptedAt = &now
	if err := storage.Update(ctx, "assignments", *assignment, tx, func(ub *sqlbuilder.UpdateBuilder) {
		ub.Where(ub.EQ("id", id))
	}); err != nil {
		return nil, err
	}
	from := order.Status
	order.Status = models.OrderAssigned
	order.DriverID = &assignment.DriverID
	order.AssignedAt = &now
	if err := storage.Update(ctx, "orders", *order, tx, func(ub *sqlbuilder.UpdateBuilder) {
		ub.Where(ub.EQ("id", order.ID))
	}); err != nil {
		return nil, err
	}
	if err := s.recordOrderStatus(ctx, tx, order.ID, from, order.Status, userID, nil); err != nil {
		return nil, err
	}
	if err := s.notify(ctx, tx, assignment.CreatedByUserID, "Назначение принято", fmt.Sprintf("Водитель принял назначение %s по заказу %s", id, order.ID)); err != nil {
		return nil, err
	}
	if order.CreatedByID != nil {
		if err := s.notify(ctx, tx, *order.CreatedByID, "Водитель назначен", fmt.Sprintf("Заказ %s принят водителем", order.ID)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return assignment, nil
}

func (s *OrderService) RejectAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.AssignmentReject) (*models.Assignment, error) {
	if role != "driver" {
		return nil, ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	_, assignment, driver, _, err := s.lockAssignmentContext(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if driver.UserID != userID {
		return nil, ErrForbidden
	}
	if assignment.Status != models.AssignmentPendingAcceptance {
		return nil, ErrAssignmentStale
	}
	now := time.Now()
	assignment.Status = models.AssignmentRejected
	assignment.RejectedAt = &now
	assignment.RejectionReasonCode = &req.ReasonCode
	assignment.RejectionComment = req.Comment
	if err := storage.Update(ctx, "assignments", *assignment, tx, func(ub *sqlbuilder.UpdateBuilder) {
		ub.Where(ub.EQ("id", id))
	}); err != nil {
		return nil, err
	}
	if err := s.notify(ctx, tx, assignment.CreatedByUserID, "Назначение отклонено", fmt.Sprintf("Водитель отклонил назначение %s по заказу %s", id, assignment.OrderID)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return assignment, nil
}

func (s *OrderService) StartAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error) {
	if role != "driver" {
		return nil, ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	order, assignment, driver, _, err := s.lockAssignmentContext(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if driver.UserID != userID {
		return nil, ErrForbidden
	}
	if assignment.Status != models.AssignmentAccepted || !models.CanTransitionOrder(order.Status, models.OrderInTransit) {
		return nil, ErrInvalidOrderTransition
	}
	now := time.Now()
	assignment.Status = models.AssignmentActive
	assignment.StartedAt = &now
	from := order.Status
	order.Status = models.OrderInTransit
	order.StartedAt = &now
	if err := storage.Update(ctx, "assignments", *assignment, tx, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", id)) }); err != nil {
		return nil, err
	}
	if err := storage.Update(ctx, "orders", *order, tx, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", order.ID)) }); err != nil {
		return nil, err
	}
	if err := s.recordOrderStatus(ctx, tx, order.ID, from, order.Status, userID, nil); err != nil {
		return nil, err
	}
	if order.CreatedByID != nil {
		if err := s.notify(ctx, tx, *order.CreatedByID, "Доставка началась", fmt.Sprintf("Водитель начал перевозку заказа %s", order.ID)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return assignment, nil
}

func (s *OrderService) ArriveAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error) {
	if role != "driver" {
		return nil, ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	order, assignment, driver, _, err := s.lockAssignmentContext(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if driver.UserID != userID {
		return nil, ErrForbidden
	}
	if assignment.Status != models.AssignmentActive || !models.CanTransitionOrder(order.Status, models.OrderArrived) {
		return nil, ErrInvalidOrderTransition
	}
	now := time.Now()
	from := order.Status
	order.Status = models.OrderArrived
	order.ArrivedAt = &now
	if err := storage.Update(ctx, "orders", *order, tx, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", order.ID)) }); err != nil {
		return nil, err
	}
	if err := s.recordOrderStatus(ctx, tx, order.ID, from, order.Status, userID, nil); err != nil {
		return nil, err
	}
	if order.CreatedByID != nil {
		if err := s.notify(ctx, tx, *order.CreatedByID, "Водитель прибыл", fmt.Sprintf("Водитель прибыл с заказом %s", order.ID)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return assignment, nil
}

func (s *OrderService) CompleteAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.DeliveryComplete) (*models.Assignment, error) {
	if role != "driver" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	order, assignment, driver, vehicle, err := s.lockAssignmentContext(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if role == "driver" && driver.UserID != userID {
		return nil, ErrForbidden
	}
	if role == "manager" {
		warehouseID, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if order.OriginWarehouseID == nil || *order.OriginWarehouseID != warehouseID {
			return nil, ErrForbidden
		}
	}
	if assignment.Status != models.AssignmentActive || !models.CanTransitionOrder(order.Status, models.OrderCompleted) {
		return nil, ErrInvalidOrderTransition
	}
	now := time.Now()
	assignment.Status = models.AssignmentCompleted
	assignment.CompletedAt = &now
	assignment.RecipientName = req.RecipientName
	assignment.DeliveryComment = req.Comment
	from := order.Status
	order.Status = models.OrderCompleted
	order.DeliveredAt = &now
	if err := storage.Update(ctx, "assignments", *assignment, tx, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", id)) }); err != nil {
		return nil, err
	}
	if err := storage.Update(ctx, "orders", *order, tx, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", order.ID)) }); err != nil {
		return nil, err
	}
	if err := s.recordOrderStatus(ctx, tx, order.ID, from, order.Status, userID, nil); err != nil {
		return nil, err
	}
	if order.CreatedByID != nil {
		if err := s.notify(ctx, tx, *order.CreatedByID, "Доставка завершена", fmt.Sprintf("Доставка заказа %s подтверждена", order.ID)); err != nil {
			return nil, err
		}
		origin := order.OriginAddress
		if origin == "" && order.OriginWarehouseID != nil {
			warehouse, err := storage.GetOne[models.Warehouse](ctx, tx, "warehouses", func(sb *sqlbuilder.SelectBuilder) {
				sb.Where(sb.EQ("id", *order.OriginWarehouseID))
			})
			if err == nil {
				origin = warehouse.Address
			}
		}
		eventID := uuid.New()
		payload, err := json.Marshal(events.DeliveryCompleted{
			EventID: eventID, OrderID: order.ID, UserID: *order.CreatedByID,
			OriginAddress: origin, DestinationAddress: order.DestinationAddress,
			CargoDescription: order.CargoDescription, WeightKg: order.WeightKg,
			VolumeM3: order.VolumeM3, TotalPrice: order.TotalPrice, DeliveredAt: now,
			RecipientName: assignment.RecipientName, DriverID: driver.ID, VehicleID: vehicle.ID,
		})
		if err != nil {
			return nil, err
		}
		if err := storage.Create(ctx, "integration_outbox", models.IntegrationEvent{
			ID: eventID, Subject: "delivery.completed.v1", Payload: payload, CreatedAt: now,
		}, tx); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return assignment, nil
}

// lockAssignmentContext is the single lock-order boundary for assignment commands.
// The first read only discovers immutable IDs; all business checks happen after
// Order -> Assignment -> Driver -> Vehicle have been locked and re-read.
func (s *OrderService) lockAssignmentContext(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*models.Order, *models.Assignment, *models.Driver, *models.Vehicle, error) {
	seed, err := storage.GetOne[models.Assignment](ctx, tx, "assignments", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id))
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", seed.OrderID)).ForUpdate()
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	assignment, err := storage.GetOne[models.Assignment](ctx, tx, "assignments", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id)).ForUpdate()
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if assignment.OrderID != seed.OrderID || assignment.DriverID != seed.DriverID || assignment.VehicleID != seed.VehicleID {
		return nil, nil, nil, nil, ErrAssignmentStale
	}
	driver, err := storage.GetOne[models.Driver](ctx, tx, "drivers", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", assignment.DriverID)).ForUpdate()
	})
	if err != nil {
		return nil, nil, nil, nil, ErrDriverNotEligible
	}
	vehicle, err := storage.GetOne[models.Vehicle](ctx, tx, "vehicles", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", assignment.VehicleID)).ForUpdate()
	})
	if err != nil {
		return nil, nil, nil, nil, ErrVehicleNotOperational
	}
	return order, assignment, driver, vehicle, nil
}

func (s *OrderService) expireStaleAssignments(ctx context.Context, orderID, driverID, vehicleID uuid.UUID) error {
	now := time.Now()
	stale, err := storage.GetAll[models.Assignment](ctx, "assignments", s.db, func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(
			sb.EQ("status", models.AssignmentPendingAcceptance),
			sb.LE("offer_expires_at", now),
			sb.Or(sb.EQ("order_id", orderID), sb.EQ("driver_id", driverID), sb.EQ("vehicle_id", vehicleID)),
		).OrderBy("id")
	})
	if err != nil {
		return err
	}
	for _, candidate := range stale {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return err
		}
		_, assignment, _, _, err := s.lockAssignmentContext(ctx, tx, candidate.ID)
		if err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return err
		}
		if assignment.Status == models.AssignmentPendingAcceptance && !now.Before(assignment.OfferExpiresAt) {
			assignment.Status = models.AssignmentExpired
			if err := storage.Update(ctx, "assignments", *assignment, tx, func(ub *sqlbuilder.UpdateBuilder) {
				ub.Where(ub.EQ("id", assignment.ID))
			}); err != nil {
				tx.Rollback(ctx) //nolint:errcheck
				return err
			}
			if err := s.notify(ctx, tx, assignment.CreatedByUserID, "Предложение истекло", fmt.Sprintf("Назначение %s по заказу %s не принято вовремя", assignment.ID, assignment.OrderID)); err != nil {
				tx.Rollback(ctx)
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *OrderService) validateAssignmentResources(ctx context.Context, tx pgx.Tx, order *models.Order, driver *models.Driver, vehicle *models.Vehicle, from, to time.Time) error {
	if driver.Status != "available" {
		return ErrDriverNotEligible
	}
	documents, err := storage.GetAll[models.DriverDocument](ctx, "driver_documents", tx, func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("driver_id", driver.ID), sb.EQ("type", "license"))
	})
	if err != nil {
		return err
	}
	if len(documents) == 0 {
		if to.After(driver.LicenseExpiry.Add(24 * time.Hour)) {
			return ErrDriverLicenseExpired
		}
	} else {
		validLicense := false
		for _, document := range documents {
			if document.Status == "valid" && !to.After(document.ValidUntil.Add(24*time.Hour)) {
				validLicense = true
				break
			}
		}
		if !validLicense {
			return ErrDriverLicenseExpired
		}
	}
	if vehicle.Status != "available" {
		return ErrVehicleNotOperational
	}
	vehicleDocuments, err := storage.GetAll[models.VehicleDocument](ctx, "vehicle_documents", tx, func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("vehicle_id", vehicle.ID), sb.EQ("type", "registration"))
	})
	if err != nil {
		return err
	}
	validRegistration := false
	for _, document := range vehicleDocuments {
		if document.Status == "valid" && !to.After(document.ValidUntil.Add(24*time.Hour)) {
			validRegistration = true
			break
		}
	}
	if !validRegistration {
		return ErrVehicleDocumentInvalid
	}
	if order.WeightKg > vehicle.CapacityKg || order.VolumeM3 > vehicle.CapacityM3 {
		return ErrVehicleCapacityExceeded
	}
	shifts, err := storage.GetAll[models.DriverShift](ctx, "driver_shifts", tx, func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("driver_id", driver.ID))
	})
	if err != nil {
		return err
	}
	if len(shifts) > 0 {
		covered := false
		for _, shift := range shifts {
			if !from.Before(shift.StartsAt) && !to.After(shift.EndsAt) {
				covered = true
				break
			}
		}
		if !covered {
			return ErrDriverOutsideShift
		}
	}
	return nil
}

func (s *OrderService) recordOrderStatus(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, from, to string, actor uuid.UUID, reason *string) error {
	history := models.OrderStatusHistory{
		ID:          uuid.New(),
		OrderID:     orderID,
		FromStatus:  &from,
		ToStatus:    to,
		ActorUserID: &actor,
		ReasonCode:  reason,
		CreatedAt:   time.Now(),
	}
	return storage.Create(ctx, "order_status_history", history, tx)
}

func (s *OrderService) notify(ctx context.Context, tx pgx.Tx, userID uuid.UUID, title, body string) error {
	return storage.Create(ctx, "notifications", models.Notification{
		ID: uuid.New(), UserID: userID, Title: title, Body: &body, CreatedAt: time.Now(),
	}, tx)
}
