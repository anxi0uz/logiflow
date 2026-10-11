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
	"github.com/jackc/pgx/v5/pgconn"
)

func orderTouchesWarehouse(order *models.Order, id uuid.UUID) bool {
	return (order.OriginWarehouseID != nil && *order.OriginWarehouseID == id) || (order.DestinationWarehouseID != nil && *order.DestinationWarehouseID == id)
}

func warehouseOperationExists(ctx context.Context, db storage.Querier, orderID uuid.UUID, kind string, assignmentID *uuid.UUID) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warehouse_operations WHERE order_id=$1 AND operation_type=$2 AND assignment_id IS NOT DISTINCT FROM $3::uuid)`, orderID, kind, assignmentID).Scan(&exists)
	return exists, err
}

func requireWarehouseOperation(ctx context.Context, db storage.Querier, orderID uuid.UUID, kind string, assignmentID *uuid.UUID) error {
	exists, err := warehouseOperationExists(ctx, db, orderID, kind, assignmentID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrWarehouseOperationRequired
	}
	return nil
}

func (s *OrderService) CreateWarehouseOperation(ctx context.Context, orderID, userID uuid.UUID, role string, req api.WarehouseOperationCreate) (*models.WarehouseOperation, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	condition := strings.TrimSpace(req.CargoCondition)
	comment, recipient := "", ""
	if req.Comment != nil {
		comment = strings.TrimSpace(*req.Comment)
	}
	if req.RecipientName != nil {
		recipient = strings.TrimSpace(*req.RecipientName)
	}
	kind := string(req.OperationType)
	switch kind {
	case "receive", "load", "unload", "undo_load", "return_to_sender":
	default:
		return nil, ErrInvalidOrderInput
	}
	if req.PackageCount < 0 || ((kind == "receive" || kind == "load") && req.PackageCount == 0) || condition == "" || utf8.RuneCountInString(condition) > 2000 || utf8.RuneCountInString(comment) > 2000 || utf8.RuneCountInString(recipient) > 200 {
		return nil, ErrInvalidOrderInput
	}
	if ((kind == "undo_load" || kind == "return_to_sender") && comment == "") || (kind == "return_to_sender" && recipient == "") {
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
	warehouseID := order.OriginWarehouseID
	if kind == "unload" {
		warehouseID = order.DestinationWarehouseID
	}
	if warehouseID == nil {
		return nil, ErrInvalidOrderInput
	}
	if role == "manager" {
		own, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if own != *warehouseID {
			return nil, ErrForbidden
		}
	}
	var assignment *models.Assignment
	if kind == "receive" || kind == "return_to_sender" {
		if req.AssignmentId != nil {
			return nil, ErrInvalidOrderInput
		}
		if kind == "receive" {
			if order.Status != models.OrderReadyForDispatch && order.Status != models.OrderAssigned {
				return nil, ErrInvalidOrderTransition
			}
		} else {
			if order.Status != models.OrderCancelled {
				return nil, ErrInvalidOrderTransition
			}
			if err = requireWarehouseOperation(ctx, tx, orderID, "receive", nil); err != nil {
				return nil, err
			}
		}
	} else {
		if req.AssignmentId == nil || *req.AssignmentId == uuid.Nil {
			return nil, ErrInvalidOrderInput
		}
		assignment, err = storage.GetOne[models.Assignment](ctx, tx, "assignments", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", *req.AssignmentId)).ForUpdate() })
		if err != nil {
			return nil, err
		}
		if assignment.OrderID != orderID {
			return nil, ErrWarehouseOperationConflict
		}
		if kind == "load" || kind == "undo_load" {
			if order.Status != models.OrderAssigned || assignment.Status != models.AssignmentAccepted {
				return nil, ErrInvalidOrderTransition
			}
			if err = requireWarehouseOperation(ctx, tx, orderID, "receive", nil); err != nil {
				return nil, err
			}
			if kind == "load" {
				if err = requireNoOpenIncidents(ctx, tx, orderID); err != nil {
					return nil, err
				}
			} else {
				loaded, err := warehouseLoadExists(ctx, tx, orderID, assignment.ID)
				if err != nil {
					return nil, err
				}
				if !loaded {
					return nil, ErrWarehouseOperationRequired
				}
			}
		} else if order.Status != models.OrderArrived || assignment.Status != models.AssignmentActive {
			return nil, ErrInvalidOrderTransition
		}
	}
	if kind == "undo_load" {
		// Use the standard resource lock order before releasing a reservation.
		if _, err := storage.GetOne[models.Driver](ctx, tx, "drivers", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", assignment.DriverID)).ForUpdate() }); err != nil {
			return nil, err
		}
		if _, err := storage.GetOne[models.Vehicle](ctx, tx, "vehicles", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", assignment.VehicleID)).ForUpdate() }); err != nil {
			return nil, err
		}
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM warehouses WHERE id=$1 FOR SHARE`, *warehouseID).Scan(&status); err != nil {
		return nil, err
	}
	if (kind == "receive" || kind == "load") && status != "active" {
		return nil, ErrWarehouseInactive
	}
	operation := models.WarehouseOperation{ID: uuid.New(), OrderID: orderID, WarehouseID: *warehouseID, AssignmentID: req.AssignmentId, OperationType: kind, PackageCount: req.PackageCount, CargoCondition: condition, Comment: comment, RecipientName: recipient, CreatedByUserID: userID, OccurredAt: time.Now().UTC()}
	if err = storage.Create(ctx, "warehouse_operations", operation, tx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrWarehouseOperationConflict
		}
		return nil, err
	}
	if err = s.recordWarehouseDiscrepancy(ctx, tx, order, &operation); err != nil {
		return nil, err
	}
	if kind == "undo_load" {
		if err = s.undoWarehouseLoading(ctx, tx, order, assignment, userID, operation.OccurredAt); err != nil {
			return nil, err
		}
	}
	recipients := []uuid.UUID{}
	if order.CreatedByID != nil {
		recipients = append(recipients, *order.CreatedByID)
	}
	if assignment != nil {
		driver, err := storage.GetOne[models.Driver](ctx, tx, "drivers", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", assignment.DriverID)) })
		if err != nil {
			return nil, err
		}
		recipients = append(recipients, driver.UserID)
	}
	labels := map[string]string{"receive": "Груз принят складом отправления", "load": "Склад подтвердил погрузку", "unload": "Склад назначения принял груз", "undo_load": "Груз снят с машины до выезда", "return_to_sender": "Груз возвращён отправителю"}
	for _, recipient := range recipients {
		if err = s.notify(ctx, tx, recipient, labels[kind], fmt.Sprintf("Заказ %s: мест %d, состояние: %s", orderID, operation.PackageCount, condition)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &operation, nil
}

func (s *OrderService) undoWarehouseLoading(ctx context.Context, tx pgx.Tx, order *models.Order, assignment *models.Assignment, actor uuid.UUID, now time.Time) error {
	reason := "loading_undone"
	assignment.Status, assignment.ReleasedAt, assignment.ReleaseReasonCode = models.AssignmentReleased, &now, &reason
	if err := storage.Update(ctx, "assignments", *assignment, tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", assignment.ID)) }); err != nil {
		return err
	}
	from := order.Status
	order.Status, order.DriverID, order.AssignedAt = models.OrderReadyForDispatch, nil, nil
	if err := storage.Update(ctx, "orders", *order, tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", order.ID)) }); err != nil {
		return err
	}
	if err := s.recordOrderStatus(ctx, tx, order.ID, from, order.Status, actor, &reason); err != nil {
		return err
	}
	return s.enqueueDispatchAfterRelease(ctx, tx, assignment)
}

func (s *OrderService) ListOrderWarehouseOperations(ctx context.Context, orderID, userID uuid.UUID, role string) ([]models.WarehouseOperation, error) {
	if _, err := s.GetOrder(ctx, orderID, userID, role); err != nil {
		return nil, err
	}
	return storage.GetAll[models.WarehouseOperation](ctx, "warehouse_operations", s.db, func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("order_id", orderID)).OrderBy("occurred_at", "id") })
}

func (s *OrderService) ListWarehouseQueue(ctx context.Context, warehouseID, userID uuid.UUID, role string, params api.ListWarehouseQueueParams) ([]models.WarehouseQueueItem, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	if role == "manager" {
		own, err := s.managerWarehouseID(ctx, s.db, userID)
		if err != nil {
			return nil, err
		}
		if own != warehouseID {
			return nil, ErrForbidden
		}
	}
	if _, err := storage.GetOne[models.Warehouse](ctx, s.db, "warehouses", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", warehouseID)) }); err != nil {
		return nil, err
	}
	limit, offset := 50, 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 10000 {
		return nil, ErrInvalidOrderInput
	}
	orders, err := storage.GetAll[models.Order](ctx, "orders", s.db, func(b *sqlbuilder.SelectBuilder) {
		b.Where(b.Or(b.EQ("origin_warehouse_id", warehouseID), b.EQ("destination_warehouse_id", warehouseID)))
		if params.IncludeClosed == nil || !*params.IncludeClosed {
			b.Where(b.Or(b.NotIn("status", models.OrderCompleted, models.OrderCancelled),
				"(status='cancelled' AND origin_warehouse_id="+b.Var(warehouseID)+" AND EXISTS(SELECT 1 FROM warehouse_operations w WHERE w.order_id=orders.id AND w.operation_type='receive') AND NOT EXISTS(SELECT 1 FROM warehouse_operations r WHERE r.order_id=orders.id AND r.operation_type='return_to_sender'))",
				"EXISTS(SELECT 1 FROM order_incidents i WHERE i.order_id=orders.id AND i.status='open')"))
		}
		b.OrderBy("created_at", "id").Limit(limit).Offset(offset)
	})
	if err != nil {
		return nil, err
	}
	result := make([]models.WarehouseQueueItem, 0, len(orders))
	if len(orders) == 0 {
		return result, nil
	}
	ids := make([]interface{}, len(orders))
	for i, order := range orders {
		ids[i] = order.ID
	}
	operations, err := storage.GetAll[models.WarehouseOperation](ctx, "warehouse_operations", s.db, func(b *sqlbuilder.SelectBuilder) {
		b.Where(b.In("order_id", ids...), b.EQ("warehouse_id", warehouseID))
	})
	if err != nil {
		return nil, err
	}
	incidents, err := storage.GetAll[models.OrderIncident](ctx, "order_incidents", s.db, func(b *sqlbuilder.SelectBuilder) { b.Where(b.In("order_id", ids...), b.EQ("status", "open")) })
	if err != nil {
		return nil, err
	}
	openCounts := map[uuid.UUID]int{}
	for _, incident := range incidents {
		openCounts[incident.OrderID]++
	}
	loads := map[uuid.UUID]map[uuid.UUID]bool{}
	undone := map[uuid.UUID]bool{}
	seen := map[uuid.UUID]map[string]bool{}
	for _, op := range operations {
		if seen[op.OrderID] == nil {
			seen[op.OrderID] = map[string]bool{}
		}
		seen[op.OrderID][op.OperationType] = true
		if op.OperationType == "load" && op.AssignmentID != nil {
			if loads[op.OrderID] == nil {
				loads[op.OrderID] = map[uuid.UUID]bool{}
			}
			loads[op.OrderID][*op.AssignmentID] = true
		}
		if op.OperationType == "undo_load" && op.AssignmentID != nil {
			undone[*op.AssignmentID] = true
		}
	}
	for _, order := range orders {
		stage := "expected"
		ops := seen[order.ID]
		if order.OriginWarehouseID != nil && *order.OriginWarehouseID == warehouseID {
			stage = "awaiting_receipt"
			if ops["receive"] {
				stage = "awaiting_assignment"
			}
			if order.Status == models.OrderAssigned && ops["receive"] {
				stage = "awaiting_loading"
			}
			loaded := false
			for id := range loads[order.ID] {
				if !undone[id] {
					loaded = true
				}
			}
			if loaded {
				stage = "ready_to_depart"
			}
			if order.StartedAt != nil {
				stage = "departed"
			}
		} else if order.Status == models.OrderArrived {
			stage = "awaiting_unloading"
		}
		if ops["unload"] {
			stage = "received"
		}
		if order.Status == models.OrderCompleted || order.Status == models.OrderCancelled {
			stage = order.Status
		}
		if order.Status == models.OrderCancelled && order.OriginWarehouseID != nil && *order.OriginWarehouseID == warehouseID && ops["receive"] {
			stage = "awaiting_return"
			if ops["return_to_sender"] {
				stage = "returned_to_sender"
			}
		}
		if openCounts[order.ID] > 0 {
			stage = "incident_open"
		}
		result = append(result, models.WarehouseQueueItem{Order: order, WarehouseStage: stage, OpenIncidentCount: openCounts[order.ID]})
	}
	return result, nil
}
