package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
)

func (s *OrderService) GetDeliveryReceipt(ctx context.Context, orderID, userID uuid.UUID, role string) (*models.DeliveryReceipt, error) {
	if _, err := s.GetOrder(ctx, orderID, userID, role); err != nil {
		return nil, err
	}
	return storage.GetOne[models.DeliveryReceipt](ctx, s.db, "delivery_receipts", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("order_id", orderID)) })
}

func (s *OrderService) CreateDeliveryReceipt(ctx context.Context, orderID, userID uuid.UUID, role string, req api.DeliveryReceiptCreate) (*models.DeliveryReceipt, error) {
	if role != "driver" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	now := time.Now().UTC()
	item := models.DeliveryReceipt{ID: uuid.New(), OrderID: orderID, AssignmentID: req.AssignmentId, RecipientName: strings.TrimSpace(req.RecipientName), RecipientRole: strings.TrimSpace(req.RecipientRole), AuthorityBasis: strings.TrimSpace(req.AuthorityBasis), PackageCount: req.PackageCount, CargoCondition: strings.TrimSpace(req.CargoCondition), SealNumber: stringValue(req.SealNumber), Comment: stringValue(req.Comment), ReceivedAt: now, RecordedAt: now, RecordedByUserID: userID}
	if req.ReceivedAt != nil {
		item.ReceivedAt = req.ReceivedAt.UTC()
	}
	if req.AssignmentId == uuid.Nil || !boundedText(item.RecipientName, 1, 200) || !boundedText(item.RecipientRole, 1, 200) || !boundedText(item.AuthorityBasis, 1, 1000) || !boundedText(item.CargoCondition, 1, 2000) || !boundedText(item.SealNumber, 0, 100) || !boundedText(item.Comment, 0, 2000) || item.PackageCount < 0 || item.ReceivedAt.After(now.Add(5*time.Minute)) {
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
	assignment, err := storage.GetOne[models.Assignment](ctx, tx, "assignments", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", req.AssignmentId)).ForUpdate() })
	if err != nil {
		return nil, err
	}
	if assignment.OrderID != orderID {
		return nil, ErrAssignmentStale
	}
	if role == "driver" {
		var belongs bool
		if err = tx.QueryRow(ctx, `SELECT user_id=$1 FROM drivers WHERE id=$2`, userID, assignment.DriverID).Scan(&belongs); err != nil {
			return nil, err
		}
		if !belongs || order.DestinationWarehouseID != nil {
			return nil, ErrForbidden
		}
	}
	if role == "manager" {
		warehouseID, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		responsible := order.OriginWarehouseID
		if order.DestinationWarehouseID != nil {
			responsible = order.DestinationWarehouseID
		}
		if responsible == nil || *responsible != warehouseID {
			return nil, ErrForbidden
		}
	}
	if order.Status != models.OrderArrived || assignment.Status != models.AssignmentActive || order.ArrivedAt == nil {
		return nil, ErrInvalidOrderTransition
	}
	if item.ReceivedAt.Before(*order.ArrivedAt) {
		return nil, ErrInvalidOrderInput
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM delivery_receipts WHERE order_id=$1)`, orderID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrDeliveryReceiptConflict
	}
	var expected *int
	if order.DestinationWarehouseID != nil {
		unload, err := storage.GetOne[models.WarehouseOperation](ctx, tx, "warehouse_operations", func(b *sqlbuilder.SelectBuilder) {
			b.Where(b.EQ("order_id", orderID), b.EQ("assignment_id", assignment.ID), b.EQ("operation_type", "unload"))
		})
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrWarehouseOperationRequired
		}
		if err != nil {
			return nil, err
		}
		if unload.PackageCount != item.PackageCount {
			return nil, ErrInvalidOrderInput
		}
	} else {
		var loaded int
		err := tx.QueryRow(ctx, `SELECT l.package_count FROM warehouse_operations l WHERE l.order_id=$1 AND l.operation_type='load' AND NOT EXISTS(SELECT 1 FROM warehouse_operations u WHERE u.assignment_id=l.assignment_id AND u.operation_type='undo_load') ORDER BY l.occurred_at DESC,l.id DESC LIMIT 1`, orderID).Scan(&loaded)
		if err == nil {
			expected = &loaded
		} else if errors.Is(err, pgx.ErrNoRows) {
			details, err := readTransportDetails(order)
			if err != nil {
				return nil, err
			}
			if details != nil {
				expected = &details.PackageCount
			}
		} else {
			return nil, err
		}
	}
	if err = storage.Create(ctx, "delivery_receipts", item, tx); err != nil {
		return nil, err
	}
	if expected != nil && *expected != item.PackageCount {
		_, err = s.saveIncident(ctx, tx, order, models.OrderIncident{ID: uuid.New(), OrderID: orderID, AssignmentID: &assignment.ID, WarehouseID: order.OriginWarehouseID, Kind: "quantity_discrepancy", Description: fmt.Sprintf("Приёмка %s: ожидалось %d мест, принято %d", item.ID, *expected, item.PackageCount), ExpectedPackages: expected, ActualPackages: &item.PackageCount, Status: "open", ReportedByUserID: userID, ReportedAt: now})
		if err != nil {
			return nil, err
		}
	}
	if order.CreatedByID != nil {
		if err = s.notify(ctx, tx, *order.CreatedByID, "Приёмка груза зафиксирована", fmt.Sprintf("Заказ %s: принял %s, мест %d; завершение перевозки подтверждается отдельно", orderID, item.RecipientName, item.PackageCount)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &item, nil
}
