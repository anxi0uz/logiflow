package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
)

func requireNoOpenIncidents(ctx context.Context, db storage.Querier, orderID uuid.UUID) error {
	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_incidents WHERE order_id=$1 AND status='open')`, orderID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrIncidentOpen
	}
	return nil
}

// A load remains a historical fact after undo_load; only its current effect ends.
func warehouseLoadExists(ctx context.Context, db storage.Querier, orderID, assignmentID uuid.UUID) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warehouse_operations l WHERE l.order_id=$1 AND l.assignment_id=$2 AND l.operation_type='load' AND NOT EXISTS(SELECT 1 FROM warehouse_operations u WHERE u.order_id=l.order_id AND u.assignment_id=l.assignment_id AND u.operation_type='undo_load'))`, orderID, assignmentID).Scan(&exists)
	return exists, err
}

func (s *OrderService) saveIncident(ctx context.Context, tx pgx.Tx, order *models.Order, incident models.OrderIncident) (*models.OrderIncident, error) {
	if err := storage.Create(ctx, "order_incidents", incident, tx); err != nil {
		return nil, err
	}
	recipients := map[uuid.UUID]bool{}
	if order.CreatedByID != nil {
		recipients[*order.CreatedByID] = true
	}
	if incident.WarehouseID != nil {
		managers, err := storage.GetAll[models.Manager](ctx, "managers", tx, func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("warehouse_id", *incident.WarehouseID)) })
		if err != nil {
			return nil, err
		}
		for _, manager := range managers {
			recipients[manager.UserID] = true
		}
	}
	if incident.AssignmentID != nil {
		var driverUser, creator uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT d.user_id,a.created_by_user_id FROM assignments a JOIN drivers d ON d.id=a.driver_id WHERE a.id=$1`, *incident.AssignmentID).Scan(&driverUser, &creator); err != nil {
			return nil, err
		}
		recipients[driverUser], recipients[creator] = true, true
	}
	for id := range recipients {
		if err := s.notify(ctx, tx, id, "Проблема перевозки", fmt.Sprintf("Заказ %s; проблема %s (%s): %s", order.ID, incident.ID, incident.Kind, incident.Description)); err != nil {
			return nil, err
		}
	}
	return &incident, nil
}

func (s *OrderService) CreateOrderIncident(ctx context.Context, orderID, userID uuid.UUID, role string, req api.OrderIncidentCreate) (*models.OrderIncident, error) {
	if role != "client" && role != "manager" && role != "driver" && role != "admin" {
		return nil, ErrForbidden
	}
	description := strings.TrimSpace(req.Description)
	kind := string(req.Kind)
	if description == "" || utf8.RuneCountInString(description) > 2000 {
		return nil, ErrInvalidOrderInput
	}
	switch kind {
	case "vehicle_breakdown", "cargo_damage", "quantity_discrepancy", "recipient_refusal", "other":
	default:
		return nil, ErrInvalidOrderInput
	}
	if (req.ExpectedPackages != nil && *req.ExpectedPackages < 0) || (req.ActualPackages != nil && *req.ActualPackages < 0) {
		return nil, ErrInvalidOrderInput
	}
	if kind == "quantity_discrepancy" && (req.ExpectedPackages == nil || req.ActualPackages == nil || *req.ExpectedPackages == *req.ActualPackages) {
		return nil, ErrInvalidOrderInput
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
	if role == "client" && (order.CreatedByID == nil || *order.CreatedByID != userID) {
		return nil, ErrForbidden
	}
	if role == "manager" {
		own, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if !orderTouchesWarehouse(order, own) {
			return nil, ErrForbidden
		}
	}
	assignment, err := storage.GetOne[models.Assignment](ctx, tx, "assignments", func(b *sqlbuilder.SelectBuilder) {
		b.Where(b.EQ("order_id", orderID), b.In("status", models.AssignmentAccepted, models.AssignmentActive)).ForUpdate()
	})
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	if role == "driver" {
		if assignment == nil {
			return nil, ErrForbidden
		}
		var belongs bool
		if err := tx.QueryRow(ctx, `SELECT user_id=$1 FROM drivers WHERE id=$2`, userID, assignment.DriverID).Scan(&belongs); err != nil {
			return nil, err
		}
		if !belongs {
			return nil, ErrForbidden
		}
	}
	if order.Status != models.OrderReadyForDispatch && order.Status != models.OrderAssigned && order.Status != models.OrderInTransit && order.Status != models.OrderArrived {
		return nil, ErrInvalidOrderTransition
	}
	if (kind == "vehicle_breakdown" && assignment == nil) || (kind == "recipient_refusal" && order.Status != models.OrderArrived) {
		return nil, ErrInvalidOrderTransition
	}
	warehouseID := order.OriginWarehouseID
	if order.Status == models.OrderArrived && kind != "vehicle_breakdown" && order.DestinationWarehouseID != nil {
		warehouseID = order.DestinationWarehouseID
	}
	item := models.OrderIncident{ID: uuid.New(), OrderID: orderID, WarehouseID: warehouseID, Kind: kind, Description: description, ExpectedPackages: req.ExpectedPackages, ActualPackages: req.ActualPackages, Status: "open", ReportedByUserID: userID, ReportedAt: time.Now().UTC()}
	if assignment != nil {
		item.AssignmentID = &assignment.ID
	}
	saved, err := s.saveIncident(ctx, tx, order, item)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

func (s *OrderService) ListOrderIncidents(ctx context.Context, orderID, userID uuid.UUID, role string) ([]models.OrderIncident, error) {
	if _, err := s.GetOrder(ctx, orderID, userID, role); err != nil {
		return nil, err
	}
	return storage.GetAll[models.OrderIncident](ctx, "order_incidents", s.db, func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("order_id", orderID)).OrderBy("reported_at", "id") })
}

func (s *OrderService) ResolveOrderIncident(ctx context.Context, id, userID uuid.UUID, role string, req api.OrderIncidentResolve) (*models.OrderIncident, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	note := strings.TrimSpace(req.Note)
	recipient := ""
	if req.RecipientName != nil {
		recipient = strings.TrimSpace(*req.RecipientName)
	}
	if note == "" || utf8.RuneCountInString(note) > 2000 || utf8.RuneCountInString(recipient) > 200 {
		return nil, ErrInvalidOrderInput
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	seed, err := storage.GetOne[models.OrderIncident](ctx, tx, "order_incidents", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", id)) })
	if err != nil {
		return nil, err
	}
	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", seed.OrderID)).ForUpdate() })
	if err != nil {
		return nil, err
	}
	item, err := storage.GetOne[models.OrderIncident](ctx, tx, "order_incidents", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", id)).ForUpdate() })
	if err != nil {
		return nil, err
	}
	if role == "manager" {
		own, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if item.WarehouseID == nil || *item.WarehouseID != own {
			return nil, ErrForbidden
		}
	}
	if item.Status != "open" {
		return nil, ErrIncidentStale
	}
	resolution := string(req.Resolution)
	valid := false
	switch item.Kind {
	case "quantity_discrepancy":
		valid = resolution == "accept_discrepancy"
	case "cargo_damage":
		valid = resolution == "accept_with_remarks" || resolution == "remediated"
	case "other":
		valid = resolution == "remediated"
	case "vehicle_breakdown":
		valid = resolution == "repaired"
		if resolution == "resources_replaced" && item.AssignmentID != nil {
			var replaced bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assignments old JOIN assignments current ON current.order_id=old.order_id WHERE old.id=$1 AND old.status='released' AND current.status IN ('accepted','active') AND old.vehicle_id<>current.vehicle_id)`, *item.AssignmentID).Scan(&replaced); err != nil {
				return nil, err
			}
			valid = replaced
		}
	case "recipient_refusal":
		valid = resolution == "recipient_accepted" && recipient != "" && order.Status == models.OrderArrived
		if valid && order.DestinationWarehouseID != nil {
			var unloaded bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warehouse_operations w JOIN assignments a ON a.id=w.assignment_id WHERE w.order_id=$1 AND w.operation_type='unload' AND a.status='active')`, order.ID).Scan(&unloaded); err != nil {
				return nil, err
			}
			valid = unloaded
		}
	}
	if !valid {
		return nil, ErrIncidentResolutionInvalid
	}
	now := time.Now().UTC()
	item.Status, item.Resolution, item.ResolutionNote, item.RecipientName = "resolved", resolution, note, recipient
	item.ResolvedByUserID, item.ResolvedAt = &userID, &now
	if err = storage.Update(ctx, "order_incidents", *item, tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", id)) }); err != nil {
		return nil, err
	}
	recipients := map[uuid.UUID]bool{item.ReportedByUserID: true}
	if order.CreatedByID != nil {
		recipients[*order.CreatedByID] = true
	}
	for recipientID := range recipients {
		if err = s.notify(ctx, tx, recipientID, "Решение по проблеме", fmt.Sprintf("Заказ %s; проблема %s: %s. %s", order.ID, id, resolution, note)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *OrderService) recordWarehouseDiscrepancy(ctx context.Context, tx pgx.Tx, order *models.Order, op *models.WarehouseOperation) error {
	var expected int
	var err error
	switch op.OperationType {
	case "receive":
		details, readErr := readTransportDetails(order)
		if readErr != nil {
			return readErr
		}
		if details == nil {
			return nil
		}
		expected = details.PackageCount
	case "undo_load":
		err = tx.QueryRow(ctx, `SELECT package_count FROM warehouse_operations WHERE order_id=$1 AND assignment_id=$2 AND operation_type='load'`, order.ID, *op.AssignmentID).Scan(&expected)
	case "load", "return_to_sender":
		err = tx.QueryRow(ctx, `SELECT package_count FROM warehouse_operations WHERE order_id=$1 AND operation_type='receive'`, order.ID).Scan(&expected)
	default:
		err = tx.QueryRow(ctx, `SELECT l.package_count FROM warehouse_operations l WHERE l.order_id=$1 AND l.operation_type='load' AND NOT EXISTS(SELECT 1 FROM warehouse_operations u WHERE u.assignment_id=l.assignment_id AND u.operation_type='undo_load') ORDER BY l.occurred_at DESC,l.id DESC LIMIT 1`, order.ID).Scan(&expected)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} // Historical trips may predate warehouse confirmations.
	if err != nil {
		return err
	}
	if expected == op.PackageCount {
		return nil
	}
	actual := op.PackageCount
	_, err = s.saveIncident(ctx, tx, order, models.OrderIncident{ID: uuid.New(), OrderID: order.ID, AssignmentID: op.AssignmentID, WarehouseID: &op.WarehouseID, SourceOperationID: &op.ID, Kind: "quantity_discrepancy", Description: fmt.Sprintf("Операция %s: ожидалось %d мест, зафиксировано %d", op.OperationType, expected, actual), ExpectedPackages: &expected, ActualPackages: &actual, Status: "open", ReportedByUserID: op.CreatedByUserID, ReportedAt: op.OccurredAt})
	return err
}
